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
	Use:           "mem",
	Short:         "MEM is your local technical oracle",
	Long:          `A CLI-first "second brain" that captures your terminal sessions, notes, and configs, making them searchable through local AI.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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

var askFlags struct {
	limit int
}

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Search using natural language",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		const defaultModel = "nomic-embed-text"
		const defaultDims = 768

		embClient := embeddings.NewClient(defaultModel)

		vecStore, err := vector.NewStore(defaultDims)
		if err != nil {
			return fmt.Errorf("opening vector store: %w", err)
		}
		defer vecStore.Close()

		question := strings.Join(args, " ")

		queryVec, err := embClient.Embed(question)
		if err != nil {
			return fmt.Errorf("generating query embedding (ensure Ollama is running with '%s'): %w", defaultModel, err)
		}

		limit := askFlags.limit
		if limit <= 0 {
			limit = 5
		}

		candidateLimit := limit * 3
		if candidateLimit < 10 {
			candidateLimit = 10
		}

		memoryIDs, err := vecStore.Search(context.Background(), queryVec, candidateLimit)
		if err != nil {
			return fmt.Errorf("searching vector store: %w", err)
		}

		if len(memoryIDs) == 0 {
			fmt.Println("No matching memories found.")
			return nil
		}

		memories, err := store.GetMemoriesByIDs(memoryIDs)
		if err != nil {
			return fmt.Errorf("retrieving memories: %w", err)
		}

		if len(memories) == 0 {
			fmt.Println("No matching memories found.")
			return nil
		}

		if len(memories) > limit {
			memories = memories[:limit]
		}

		fmt.Println(formatSearchResults(memories))
		return nil
	},
}

var listFlags struct {
	tag   string
	limit int
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List stored memories",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		memories, err := store.ListMemories(listFlags.tag, listFlags.limit)
		if err != nil {
			return fmt.Errorf("listing memories: %w", err)
		}

		if len(memories) == 0 {
			fmt.Println("No memories found.")
			return nil
		}

		fmt.Println(formatSearchResults(memories))
		return nil
	},
}

var showCmd = &cobra.Command{
	Use:     "show [id]",
	Aliases: []string{"get", "view"},
	Short:   "Show details of a memory by ID",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int64
		if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
			return fmt.Errorf("invalid memory ID: %s", args[0])
		}

		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		m, err := store.GetMemory(id)
		if err != nil {
			return err
		}

		fmt.Printf("#%d: %s\n", m.ID, m.Title)
		if len(m.Tags) > 0 {
			fmt.Printf("Tags: %s\n", strings.Join(m.Tags, ", "))
		}
		if m.Description != "" {
			fmt.Printf("Description: %s\n", m.Description)
		}
		fmt.Printf("Created: %s | Source: %s\n", m.CreatedAt.Format("2006-01-02 15:04:05"), m.Source)

		if len(m.Commands) > 0 {
			fmt.Println("Commands:")
			for _, cmd := range m.Commands {
				fmt.Printf("  $ %s\n", cmd.Command)
			}
		}

		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:     "delete [id]",
	Aliases: []string{"rm", "remove"},
	Short:   "Delete a memory by ID",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int64
		if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
			return fmt.Errorf("invalid memory ID: %s", args[0])
		}

		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		if err := store.DeleteMemory(id); err != nil {
			return fmt.Errorf("deleting memory: %w", err)
		}

		const defaultDims = 768
		vecStore, err := vector.NewStore(defaultDims)
		if err == nil {
			defer vecStore.Close()
			_ = vecStore.Delete(context.Background(), id)
		}

		fmt.Printf("Deleted memory #%d\n", id)
		return nil
	},
}

func formatSearchResults(memories []storage.Memory) string {
	if len(memories) == 0 {
		return "No matching memories found."
	}

	var sb strings.Builder
	for i, m := range memories {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		sb.WriteString(fmt.Sprintf("%d. %s", i+1, m.Title))
		if len(m.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("  [%s]", strings.Join(m.Tags, ", ")))
		}

		if m.Description != "" {
			sb.WriteString("\n   ")
			sb.WriteString(m.Description)
		}

		for _, cmd := range m.Commands {
			sb.WriteString("\n   $ ")
			sb.WriteString(cmd.Command)
		}
	}
	return sb.String()
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

	askCmd.Flags().IntVarP(&askFlags.limit, "limit", "n", 5, "maximum number of results to return")

	listCmd.Flags().StringVarP(&listFlags.tag, "tag", "t", "", "filter by tag")
	listCmd.Flags().IntVarP(&listFlags.limit, "limit", "n", 20, "maximum number of items to list")

	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(rememberCmd)
}
