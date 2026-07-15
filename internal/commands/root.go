package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/meidori/mem/internal/embeddings"
	"github.com/meidori/mem/internal/storage"
	"github.com/meidori/mem/internal/vector"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mem",
	Short: "MEM is your local technical oracle",
	Long:  `A CLI-first "second brain" that captures your terminal sessions, notes, and configs, making them searchable through local AI.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var saveFlags struct {
	commands []string
	tags     []string
}

var saveCmd = &cobra.Command{
	Use:   "save [title]",
	Short: "Capture a new memory",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		// Initialize Embeddings and Vector DB
		const defaultModel = "nomic-embed-text"
		const defaultDims = 768

		embClient := embeddings.NewClient(defaultModel)
		
		vecStore, err := vector.NewStore(defaultDims)
		if err != nil {
			return fmt.Errorf("opening vector store: %w", err)
		}
		defer vecStore.Close()

		// Get or create embedding config
		embConfig, err := store.GetOrCreateActiveEmbeddingConfig(defaultModel, defaultDims)
		if err != nil {
			return fmt.Errorf("getting active embedding config: %w", err)
		}

		title := strings.Join(args, " ")
		m := &storage.Memory{
			Title:  title,
			Source: storage.SourceSave,
			Tags:   saveFlags.tags,
		}
		for _, c := range saveFlags.commands {
			m.Commands = append(m.Commands, storage.Command{Command: c})
		}

		if err := store.SaveMemory(m); err != nil {
			return fmt.Errorf("saving memory: %w", err)
		}

		// Process chunks
		chunks := ChunkMemory(m)
		for _, chunk := range chunks {
			// Check if already indexed with this hash
			status, err := store.GetEmbeddingStatus(m.ID, chunk.CommandID, chunk.ChunkIndex, embConfig.ID)
			if err != nil {
				return fmt.Errorf("getting embedding status: %w", err)
			}
			if status != nil && status.ContentHash == chunk.Hash && !status.NeedsReindex {
				continue // already indexed
			}

			// Generate embedding
			vec, err := embClient.Embed(chunk.Text)
			if err != nil {
				return fmt.Errorf("generating embedding: %w", err)
			}

			// Insert into LanceDB
			if err := vecStore.Insert(context.Background(), m.ID, vec); err != nil {
				return fmt.Errorf("inserting into vector store: %w", err)
			}

			// Update SQLite status
			newStatus := &storage.EmbeddingStatus{
				MemoryID:          m.ID,
				CommandID:         chunk.CommandID,
				EmbeddingConfigID: embConfig.ID,
				ChunkIndex:        chunk.ChunkIndex,
				ContentHash:       chunk.Hash,
			}
			if err := store.UpsertEmbeddingStatus(newStatus); err != nil {
				return fmt.Errorf("upserting embedding status: %w", err)
			}
		}

		fmt.Printf("Saved: %s\n", title)
		return nil
	},
}

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Search using natural language",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Asking AI: %s\n", args[0])
	},
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Record terminal session",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Watching terminal...")
	},
}

var rememberCmd = &cobra.Command{
	Use:   "remember [description]",
	Short: "Transform terminal history into a memory",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Remembering session: %s\n", args[0])
	},
}

func init() {
	saveCmd.Flags().StringArrayVarP(&saveFlags.commands, "command", "c", nil, "shell command to attach")
	saveCmd.Flags().StringArrayVarP(&saveFlags.tags, "tag", "t", nil, "tag to assign")

	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(rememberCmd)
}
