package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/meidori/mem/internal/embeddings"
	"github.com/meidori/mem/internal/storage"
	"github.com/meidori/mem/internal/vector"
	"github.com/spf13/cobra"
)

const reindexBatchSize = 200

var reindexFlags struct {
	all   bool
	model string
}

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild the semantic search index",
	Long: `Re-embed memories whose vectors are missing or stale, e.g. ones saved while
Ollama was down. Safe to interrupt and re-run: finished memories are skipped.

  mem reindex                           # only missing/stale memories
  mem reindex --all                     # rebuild the whole index
  mem reindex --model mxbai-embed-large # switch embedding model (full rebuild)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		cfg, err := activeConfig(store)
		if err != nil {
			return err
		}

		var vecStore *vector.Store
		if model := reindexFlags.model; model != "" && model != cfg.ModelName {
			emb := embeddings.NewClient(model)
			probe, err := emb.Embed("dimension probe")
			if err != nil {
				return fmt.Errorf("probing model %q (did you `ollama pull %s`?): %w", model, model, err)
			}
			if len(probe) == 0 {
				return fmt.Errorf("model %q returned an empty embedding", model)
			}

			// Flag everything stale before dropping vectors so an interrupted
			// rebuild resumes where it stopped.
			if cfg, err = store.ActivateEmbeddingConfig(model, len(probe)); err != nil {
				return err
			}
			if vecStore, err = vector.Reset(cfg.Dimensions); err != nil {
				return fmt.Errorf("resetting vector store: %w", err)
			}
			fmt.Printf("Switched embedding model to %s (%d dims).\n", cfg.ModelName, cfg.Dimensions)
		} else {
			vecStore, err = vector.NewStore(cfg.Dimensions)
			if reindexFlags.all || errors.Is(err, vector.ErrDimsMismatch) {
				if vecStore != nil {
					vecStore.Close()
				}
				if err := store.MarkAllForReindex(); err != nil {
					return err
				}
				vecStore, err = vector.Reset(cfg.Dimensions)
			}
			if err != nil {
				return fmt.Errorf("opening vector store: %w", err)
			}
		}
		defer vecStore.Close()

		ids, err := store.AllMemoryIDs()
		if err != nil {
			return err
		}

		emb := embeddings.NewClient(cfg.ModelName)
		ctx := context.Background()
		indexed, upToDate := 0, 0
		for start := 0; start < len(ids); start += reindexBatchSize {
			end := min(start+reindexBatchSize, len(ids))
			memories, err := store.GetMemoriesByIDs(ids[start:end])
			if err != nil {
				return fmt.Errorf("loading memories: %w", err)
			}
			for i := range memories {
				m := &memories[i]
				changed, err := indexMemory(ctx, store, emb, vecStore, cfg, m)
				if err != nil {
					return fmt.Errorf("reindexing #%d after %d memories: %w", m.ID, indexed, err)
				}
				if changed {
					indexed++
					fmt.Printf("  indexed #%d: %s\n", m.ID, m.Title)
				} else {
					upToDate++
				}
			}
		}

		fmt.Printf("Reindexed %d memories with %s (%d already up to date).\n", indexed, cfg.ModelName, upToDate)
		return nil
	},
}

func init() {
	reindexCmd.Flags().BoolVar(&reindexFlags.all, "all", false, "drop the vector index and re-embed every memory")
	reindexCmd.Flags().StringVar(&reindexFlags.model, "model", "", "switch to another Ollama embedding model and rebuild")
}
