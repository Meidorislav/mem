package vector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	lancedb "github.com/lancedb/lancedb-go/pkg/lancedb"
)

const tableName = "embeddings"

type Store struct {
	db    contracts.IConnection
	table contracts.ITable
	dims  int
}

func NewStore(dims int) (*Store, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	return NewStoreAt(path, dims)
}

func NewStoreAt(dbPath string, dims int) (*Store, error) {
	db, err := lancedb.Connect(context.Background(), dbPath, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to lancedb: %w", err)
	}

	schema, err := lancedb.NewSchemaBuilder().
		AddInt64Field("memory_id", false).
		AddVectorField("embedding", dims, contracts.VectorDataTypeFloat32, false).
		Build()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("building schema: %w", err)
	}

	table, err := db.OpenTable(context.Background(), tableName)
	if err != nil {
		table, err = db.CreateTable(context.Background(), tableName, schema)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("creating table: %w", err)
		}
	}

	s := &Store{db: db, table: table, dims: dims}
	if err := s.checkDims(context.Background()); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// ErrDimsMismatch means the on-disk index was built by a model with a
// different vector size; it has to be rebuilt with Reset.
var ErrDimsMismatch = errors.New("vector index dimensions mismatch")

func (s *Store) checkDims(ctx context.Context) error {
	schema, err := s.table.Schema(ctx)
	if err != nil {
		return fmt.Errorf("reading table schema: %w", err)
	}
	fields, ok := schema.FieldsByName("embedding")
	if !ok || len(fields) == 0 {
		return fmt.Errorf("table %s has no embedding column", tableName)
	}
	list, ok := fields[0].Type.(*arrow.FixedSizeListType)
	if !ok {
		return fmt.Errorf("unexpected embedding column type %s", fields[0].Type)
	}
	if int(list.Len()) != s.dims {
		return fmt.Errorf("%w: index has %d, model has %d", ErrDimsMismatch, list.Len(), s.dims)
	}
	return nil
}

func (s *Store) Close() error {
	if s.table != nil {
		s.table.Close()
	}
	return s.db.Close()
}

// ResetAt drops all vectors and recreates the table for dims. Used when the
// embedding model changes, since vectors from different models are incompatible.
func ResetAt(dbPath string, dims int) (*Store, error) {
	db, err := lancedb.Connect(context.Background(), dbPath, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to lancedb: %w", err)
	}
	names, err := db.TableNames(context.Background())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	for _, name := range names {
		if name == tableName {
			if err := db.DropTable(context.Background(), tableName); err != nil {
				db.Close()
				return nil, fmt.Errorf("dropping table: %w", err)
			}
		}
	}
	db.Close()
	return NewStoreAt(dbPath, dims)
}

// Reset is ResetAt for the default location (~/.mem/vectors).
func Reset(dims int) (*Store, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	return ResetAt(path, dims)
}

func defaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	return filepath.Join(home, ".mem", "vectors"), nil
}

// Insert stores vec for memoryID. Vectors are L2-normalized so LanceDB's
// L2 ranking matches cosine similarity.
func (s *Store) Insert(ctx context.Context, memoryID int64, vec []float32) error {
	if len(vec) != s.dims {
		return fmt.Errorf("vector length %d does not match store dims %d", len(vec), s.dims)
	}

	record, err := s.buildRecord(memoryID, normalize(vec))
	if err != nil {
		return err
	}
	defer record.Release()

	return s.table.Add(ctx, record, nil)
}

// Hit is one search result: a chunk of a memory and its cosine similarity
// to the query (1 = identical direction, 0 = unrelated).
type Hit struct {
	MemoryID int64
	Score    float64
}

// Search returns the nearest chunks to vec, best first. A memory can appear
// once per indexed chunk.
func (s *Store) Search(ctx context.Context, vec []float32, limit int) ([]Hit, error) {
	query := normalize(vec)
	results, err := s.table.VectorSearch(ctx, "embedding", query, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	hits := make([]Hit, 0, len(results))
	for _, row := range results {
		id, ok := toInt64(row["memory_id"])
		if !ok {
			continue
		}
		// Score from the stored vector itself, so vectors indexed before
		// normalization was introduced still get a correct cosine.
		score, ok := cosine(query, row["embedding"])
		if !ok {
			d, _ := toFloat64(row["_distance"])
			score = 1 - d/2 // squared L2 between unit vectors
		}
		hits = append(hits, Hit{MemoryID: id, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits, nil
}

func normalize(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return vec
	}
	norm := math.Sqrt(sum)
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = float32(float64(v) / norm)
	}
	return out
}

// cosine computes the cosine similarity between unit vector q and a stored
// embedding as returned by LanceDB.
func cosine(q []float32, stored any) (float64, bool) {
	vals, ok := stored.([]any)
	if !ok || len(vals) != len(q) {
		return 0, false
	}
	var dot, norm float64
	for i, v := range vals {
		f, ok := toFloat64(v)
		if !ok {
			return 0, false
		}
		dot += float64(q[i]) * f
		norm += f * f
	}
	if norm == 0 {
		return 0, true
	}
	return dot / math.Sqrt(norm), true
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	}
	return 0, false
}

func (s *Store) Delete(ctx context.Context, memoryID int64) error {
	return s.table.Delete(ctx, fmt.Sprintf("memory_id = %d", memoryID))
}

func (s *Store) buildRecord(memoryID int64, vec []float32) (arrow.Record, error) {
	pool := memory.NewGoAllocator()

	listType := arrow.FixedSizeListOf(int32(s.dims), arrow.PrimitiveTypes.Float32)
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "memory_id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "embedding", Type: listType, Nullable: false},
	}, nil)

	idBuilder := array.NewInt64Builder(pool)
	defer idBuilder.Release()
	idBuilder.Append(memoryID)

	listBuilder := array.NewFixedSizeListBuilder(pool, int32(s.dims), arrow.PrimitiveTypes.Float32)
	defer listBuilder.Release()
	floatBuilder := listBuilder.ValueBuilder().(*array.Float32Builder)

	listBuilder.Append(true)
	floatBuilder.AppendValues(vec, nil)

	idArr := idBuilder.NewArray()
	defer idArr.Release()
	listArr := listBuilder.NewArray()
	defer listArr.Release()

	record := array.NewRecord(schema, []arrow.Array{idArr, listArr}, 1)
	return record, nil
}
