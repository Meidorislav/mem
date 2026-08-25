package vector_test

import (
	"context"
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

	if results[0] != 10 {
		t.Errorf("expected top result to be memory ID 10, got %d", results[0])
	}
	if results[1] != 30 {
		t.Errorf("expected second result to be memory ID 30, got %d", results[1])
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
