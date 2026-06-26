package vector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	lancedb "github.com/lancedb/lancedb-go/pkg/lancedb"
	"github.com/lancedb/lancedb-go/pkg/contracts"
)

const tableName = "embeddings"

type Store struct {
	db    contracts.IConnection
	table contracts.ITable
	dims  int
}

func NewStore(dims int) (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}

	dbPath := filepath.Join(home, ".mem", "vectors")
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

	return &Store{db: db, table: table, dims: dims}, nil
}

func (s *Store) Close() error {
	s.table.Close()
	return s.db.Close()
}

func (s *Store) Insert(ctx context.Context, memoryID int64, vec []float32) error {
	if len(vec) != s.dims {
		return fmt.Errorf("vector length %d does not match store dims %d", len(vec), s.dims)
	}

	record, err := s.buildRecord(memoryID, vec)
	if err != nil {
		return err
	}
	defer record.Release()

	return s.table.Add(ctx, record, nil)
}

func (s *Store) Search(ctx context.Context, vec []float32, limit int) ([]int64, error) {
	results, err := s.table.VectorSearch(ctx, "embedding", vec, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	ids := make([]int64, 0, len(results))
	for _, row := range results {
		id, ok := row["memory_id"].(int64)
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
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
