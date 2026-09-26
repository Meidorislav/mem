package commands

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/meidori/mem/internal/storage"
)

type fakeEmbedder struct {
	calls int
	err   error
}

func (f *fakeEmbedder) Embed(text string) ([]float32, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return []float32{float32(len(text)), 0, 0}, nil
}

type fakeIndex struct {
	vectors map[int64]int
	deletes int
}

func (f *fakeIndex) Insert(ctx context.Context, memoryID int64, vec []float32) error {
	f.vectors[memoryID]++
	return nil
}

func (f *fakeIndex) Delete(ctx context.Context, memoryID int64) error {
	f.deletes++
	delete(f.vectors, memoryID)
	return nil
}

func setupIndexTest(t *testing.T) (*storage.Store, *storage.EmbeddingConfig, *storage.Memory) {
	t.Helper()
	store, err := storage.NewStoreAt(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 3)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}

	// A description adds a context chunk with a NULL command_id.
	m := &storage.Memory{
		Title:       "restart api",
		Source:      storage.SourceRemember,
		Description: "api-service crash on prod",
		Commands:    []storage.Command{{Command: "journalctl -u api"}, {Command: "systemctl restart api"}},
	}
	if err := store.SaveMemory(m); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	return store, cfg, m
}

func TestIndexMemory(t *testing.T) {
	store, cfg, m := setupIndexTest(t)
	ctx := context.Background()
	emb := &fakeEmbedder{}
	idx := &fakeIndex{vectors: map[int64]int{}}

	changed, err := indexMemory(ctx, store, emb, idx, cfg, m)
	if err != nil {
		t.Fatalf("indexMemory: %v", err)
	}
	if !changed || emb.calls != 3 || idx.vectors[m.ID] != 3 {
		t.Fatalf("first index: changed=%v embeds=%d vectors=%d, want true/3/3", changed, emb.calls, idx.vectors[m.ID])
	}

	// Nothing changed: no embedding calls, vectors untouched.
	changed, err = indexMemory(ctx, store, emb, idx, cfg, m)
	if err != nil {
		t.Fatalf("indexMemory (again): %v", err)
	}
	if changed || emb.calls != 3 {
		t.Fatalf("second index: changed=%v embeds=%d, want false/3", changed, emb.calls)
	}

	// Flagged for reindex: vectors are replaced, not duplicated.
	if err := store.MarkAllForReindex(); err != nil {
		t.Fatalf("MarkAllForReindex: %v", err)
	}
	changed, err = indexMemory(ctx, store, emb, idx, cfg, m)
	if err != nil {
		t.Fatalf("indexMemory (reindex): %v", err)
	}
	if !changed || idx.vectors[m.ID] != 3 {
		t.Fatalf("reindex: changed=%v vectors=%d, want true/3", changed, idx.vectors[m.ID])
	}

	changed, _ = indexMemory(ctx, store, emb, idx, cfg, m)
	if changed {
		t.Error("index after reindex reported changes, want up to date")
	}
}

func TestIndexMemoryEmbedFailureKeepsVectors(t *testing.T) {
	store, cfg, m := setupIndexTest(t)
	ctx := context.Background()
	idx := &fakeIndex{vectors: map[int64]int{}}

	if _, err := indexMemory(ctx, store, &fakeEmbedder{}, idx, cfg, m); err != nil {
		t.Fatalf("indexMemory: %v", err)
	}
	if err := store.MarkAllForReindex(); err != nil {
		t.Fatalf("MarkAllForReindex: %v", err)
	}

	failing := &fakeEmbedder{err: errors.New("connection refused")}
	if _, err := indexMemory(ctx, store, failing, idx, cfg, m); err == nil {
		t.Fatal("indexMemory succeeded with failing embedder")
	}
	if idx.deletes != 1 || idx.vectors[m.ID] != 3 {
		t.Errorf("after failure: deletes=%d vectors=%d, want old vectors kept (1/3)", idx.deletes, idx.vectors[m.ID])
	}
}
