package vector_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/meidori/mem/internal/vector"
)

func TestVectorStore_InsertAndSearch(t *testing.T) {
	tmpDir := t.TempDir()
	vecPath := filepath.Join(tmpDir, "vectors")

	dims := 3
	store, err := vector.NewStoreAt(vecPath, dims)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert vectors
	// Vector 1: [1.0, 0.0, 0.0] (memory ID 10)
	if err := store.Insert(ctx, 10, []float32{1.0, 0.0, 0.0}); err != nil {
		t.Fatalf("Insert mem 10: %v", err)
	}

	// Vector 2: [0.0, 1.0, 0.0] (memory ID 20)
	if err := store.Insert(ctx, 20, []float32{0.0, 1.0, 0.0}); err != nil {
		t.Fatalf("Insert mem 20: %v", err)
	}

	// Vector 3: [0.9, 0.1, 0.0] (memory ID 30)
	if err := store.Insert(ctx, 30, []float32{0.9, 0.1, 0.0}); err != nil {
		t.Fatalf("Insert mem 30: %v", err)
	}

	// Search for vector closest to [1.0, 0.0, 0.0]
	// Memory 10 and 30 should be the top results
	query := []float32{1.0, 0.0, 0.0}
	results, err := store.Search(ctx, query, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].MemoryID != 10 {
		t.Errorf("expected top result to be memory ID 10, got %d", results[0].MemoryID)
	}
	if results[1].MemoryID != 30 {
		t.Errorf("expected second result to be memory ID 30, got %d", results[1].MemoryID)
	}
}

func TestVectorStore_DimensionMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	vecPath := filepath.Join(tmpDir, "vectors")

	store, err := vector.NewStoreAt(vecPath, 4)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	err = store.Insert(ctx, 1, []float32{1.0, 2.0})
	if err == nil {
		t.Fatal("expected error on dimension mismatch, got nil")
	}
}

func TestVectorStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	vecPath := filepath.Join(tmpDir, "vectors")

	dims := 3
	store, err := vector.NewStoreAt(vecPath, dims)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Insert(ctx, 42, []float32{1.0, 0.0, 0.0}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	results, err := store.Search(ctx, []float32{1.0, 0.0, 0.0}, 5)
	if err != nil || len(results) != 1 {
		t.Fatalf("Search before delete: results=%v, err=%v", results, err)
	}

	if err := store.Delete(ctx, 42); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err = store.Search(ctx, []float32{1.0, 0.0, 0.0}, 5)
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %v", results)
	}
}

func TestVectorStore_ReopenWithDifferentDims(t *testing.T) {
	vecPath := filepath.Join(t.TempDir(), "vectors")

	store, err := vector.NewStoreAt(vecPath, 3)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	store.Close()

	if _, err := vector.NewStoreAt(vecPath, 4); !errors.Is(err, vector.ErrDimsMismatch) {
		t.Fatalf("NewStoreAt with other dims: err = %v, want ErrDimsMismatch", err)
	}
}

func TestVectorStore_Reset(t *testing.T) {
	vecPath := filepath.Join(t.TempDir(), "vectors")
	ctx := context.Background()

	store, err := vector.NewStoreAt(vecPath, 3)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Insert(ctx, 1, []float32{1, 0, 0}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	store.Close()

	store, err = vector.ResetAt(vecPath, 4)
	if err != nil {
		t.Fatalf("ResetAt: %v", err)
	}
	defer store.Close()

	results, err := store.Search(ctx, []float32{1, 0, 0, 0}, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results after reset = %v, want none", results)
	}
	if err := store.Insert(ctx, 2, []float32{0, 1, 0, 0}); err != nil {
		t.Fatalf("Insert after reset: %v", err)
	}
}

func TestVectorStore_SearchScoresAreCosine(t *testing.T) {
	store, err := vector.NewStoreAt(filepath.Join(t.TempDir(), "vectors"), 2)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Magnitudes differ wildly; only direction should matter.
	if err := store.Insert(ctx, 1, []float32{30, 0}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.Insert(ctx, 2, []float32{0.1, 0.1}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.Insert(ctx, 3, []float32{0, 5}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	hits, err := store.Search(ctx, []float32{2, 0}, 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %v, want 3", hits)
	}
	want := []struct {
		id    int64
		score float64
	}{{1, 1}, {2, math.Sqrt2 / 2}, {3, 0}}
	for i, w := range want {
		if hits[i].MemoryID != w.id || math.Abs(hits[i].Score-w.score) > 1e-4 {
			t.Errorf("hits[%d] = %+v, want id %d score %.4f", i, hits[i], w.id, w.score)
		}
	}
}
