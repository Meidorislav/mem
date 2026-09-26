package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/meidori/mem/internal/embeddings"
	"github.com/meidori/mem/internal/storage"
	"github.com/meidori/mem/internal/vector"
)

const (
	defaultModel = "nomic-embed-text"
	defaultDims  = 768
)

type embedder interface {
	Embed(text string) ([]float32, error)
}

type vectorIndex interface {
	Insert(ctx context.Context, memoryID int64, vec []float32) error
	Delete(ctx context.Context, memoryID int64) error
}

// activeConfig returns the embedding model currently in use, registering the
// default model on first run.
func activeConfig(store *storage.Store) (*storage.EmbeddingConfig, error) {
	cfg, err := store.GetActiveEmbeddingConfig()
	if err != nil {
		return nil, fmt.Errorf("getting active embedding config: %w", err)
	}
	if cfg != nil {
		return cfg, nil
	}
	cfg, err = store.GetOrCreateActiveEmbeddingConfig(defaultModel, defaultDims)
	if err != nil {
		return nil, fmt.Errorf("creating default embedding config: %w", err)
	}
	return cfg, nil
}

func openVectors(cfg *storage.EmbeddingConfig) (*vector.Store, error) {
	vecStore, err := vector.NewStore(cfg.Dimensions)
	if errors.Is(err, vector.ErrDimsMismatch) {
		return nil, fmt.Errorf("opening vector store: %w (rebuild it with `mem reindex --all`)", err)
	}
	if err != nil {
		return nil, fmt.Errorf("opening vector store: %w", err)
	}
	return vecStore, nil
}

// indexNewMemory embeds a freshly saved memory with the active model.
func indexNewMemory(store *storage.Store, m *storage.Memory) error {
	cfg, err := activeConfig(store)
	if err != nil {
		return err
	}
	vecStore, err := openVectors(cfg)
	if err != nil {
		return err
	}
	defer vecStore.Close()

	emb := embeddings.NewClient(cfg.ModelName)
	_, err = indexMemory(context.Background(), store, emb, vecStore, cfg, m)
	return err
}

// indexMemory brings m's vectors up to date. Chunks are compared against
// embedding_status by content hash; if any chunk is missing, changed or
// flagged needs_reindex, all of the memory's vectors are replaced, since
// LanceDB rows only reference the memory ID. It reports whether anything was
// re-embedded.
func indexMemory(ctx context.Context, store *storage.Store, emb embedder, vec vectorIndex, cfg *storage.EmbeddingConfig, m *storage.Memory) (bool, error) {
	chunks := ChunkMemory(m)

	stale := false
	for _, chunk := range chunks {
		status, err := store.GetEmbeddingStatus(m.ID, chunk.CommandID, chunk.ChunkIndex, cfg.ID)
		if err != nil {
			return false, fmt.Errorf("getting embedding status: %w", err)
		}
		if status == nil || status.ContentHash != chunk.Hash || status.NeedsReindex {
			stale = true
			break
		}
	}
	if !stale {
		return false, nil
	}

	// Embed everything before touching the index so a failing Ollama call
	// leaves the old vectors in place.
	vecs := make([][]float32, len(chunks))
	for i, chunk := range chunks {
		v, err := emb.Embed(chunk.Text)
		if err != nil {
			return false, fmt.Errorf("generating embedding (is Ollama running with %q?): %w", cfg.ModelName, err)
		}
		vecs[i] = v
	}

	if err := vec.Delete(ctx, m.ID); err != nil {
		return false, fmt.Errorf("removing old vectors: %w", err)
	}
	for i, chunk := range chunks {
		if err := vec.Insert(ctx, m.ID, vecs[i]); err != nil {
			return false, fmt.Errorf("inserting into vector store: %w", err)
		}
		status := &storage.EmbeddingStatus{
			MemoryID:          m.ID,
			CommandID:         chunk.CommandID,
			EmbeddingConfigID: cfg.ID,
			ChunkIndex:        chunk.ChunkIndex,
			ContentHash:       chunk.Hash,
		}
		if err := store.UpsertEmbeddingStatus(status); err != nil {
			return false, fmt.Errorf("upserting embedding status: %w", err)
		}
	}
	return true, nil
}
