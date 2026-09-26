package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/meidori/mem/internal/embeddings"
	"github.com/meidori/mem/internal/llm"
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
	commands    []string
	tags        []string
	description string
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

		title := strings.Join(args, " ")
		m := &storage.Memory{
			Title:       title,
			Source:      storage.SourceSave,
			Description: saveFlags.description,
			Tags:        saveFlags.tags,
		}
		for _, c := range saveFlags.commands {
			m.Commands = append(m.Commands, storage.Command{Command: c})
		}

		if err := store.SaveMemory(m); err != nil {
			return fmt.Errorf("saving memory: %w", err)
		}

		fmt.Printf("Saved #%d: %s\n", m.ID, title)
		warnIfNotIndexed(store, m)
		return nil
	},
}

// warnIfNotIndexed indexes m and, on failure, tells the user how to recover.
// The memory itself is already in SQLite, so a missing Ollama is not fatal.
func warnIfNotIndexed(store *storage.Store, m *storage.Memory) {
	if err := indexNewMemory(store, m); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: memory #%d is not searchable yet: %v\n", m.ID, err)
		fmt.Fprintln(os.Stderr, "Run `mem reindex` once Ollama is available.")
	}
}

var askFlags struct {
	limit    int
	minScore float64
	answer   bool
	llm      string
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

		cfg, err := activeConfig(store)
		if err != nil {
			return err
		}

		vecStore, err := openVectors(cfg)
		if err != nil {
			return err
		}
		defer vecStore.Close()

		question := strings.Join(args, " ")

		queryVec, err := embeddings.NewClient(cfg.ModelName).Embed(question)
		if err != nil {
			return fmt.Errorf("generating query embedding (ensure Ollama is running with '%s'): %w", cfg.ModelName, err)
		}

		limit := askFlags.limit
		if limit <= 0 {
			limit = 5
		}

		// Multi-chunk memories return one hit per chunk, so over-fetch
		// before deduplicating by memory ID.
		candidateLimit := limit * 3
		if candidateLimit < 10 {
			candidateLimit = 10
		}

		hits, err := vecStore.Search(context.Background(), queryVec, candidateLimit)
		if err != nil {
			return fmt.Errorf("searching vector store: %w", err)
		}

		if n, err := store.CountUnindexedMemories(cfg.ID); err == nil && n > 0 {
			fmt.Fprintf(os.Stderr, "Note: %d %s not indexed yet; run `mem reindex` to include them.\n", n, plural(n, "memory is", "memories are"))
		}

		ranked := rankHits(hits, askFlags.minScore, limit)
		if len(ranked) == 0 {
			if len(hits) == 0 {
				fmt.Println("No matching memories found.")
			} else {
				fmt.Printf("No memories scored above %.2f (best: %.2f). Try a lower --min-score.\n", askFlags.minScore, hits[0].Score)
			}
			return nil
		}

		ids := make([]int64, len(ranked))
		scores := make(map[int64]float64, len(ranked))
		for i, h := range ranked {
			ids[i] = h.MemoryID
			scores[h.MemoryID] = h.Score
		}

		memories, err := store.GetMemoriesByIDs(ids)
		if err != nil {
			return fmt.Errorf("retrieving memories: %w", err)
		}

		if len(memories) == 0 {
			fmt.Println("No matching memories found.")
			return nil
		}

		if askFlags.answer {
			prompt := llm.BuildPrompt(question, memories)
			if err := llm.NewClient(askFlags.llm).Generate(prompt, os.Stdout); err != nil {
				return fmt.Errorf("generating answer (ensure Ollama is running with '%s'): %w", askFlags.llm, err)
			}
			fmt.Print("\n\nSources:\n")
		}

		fmt.Println(formatSearchResults(memories, scores))
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

		fmt.Println(formatSearchResults(memories, nil))
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

		// SQLite is the source of truth; a leftover vector is harmless since
		// search results are joined back to SQLite and unknown IDs skipped.
		if cfg, err := store.GetActiveEmbeddingConfig(); err == nil && cfg != nil {
			if vecStore, err := vector.NewStore(cfg.Dimensions); err == nil {
				defer vecStore.Close()
				_ = vecStore.Delete(context.Background(), id)
			}
		}

		fmt.Printf("Deleted memory #%d\n", id)
		return nil
	},
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// formatSearchResults renders memories as a numbered list. scores, when
// non-nil, adds each memory's similarity to the query.
func formatSearchResults(memories []storage.Memory, scores map[int64]float64) string {
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
		if score, ok := scores[m.ID]; ok {
			sb.WriteString(fmt.Sprintf("  (%.2f)", score))
		}

		if m.Description != "" {
			sb.WriteString("\n   ")
			sb.WriteString(m.Description)
		}

		for _, cmd := range m.Commands {
			sb.WriteString("\n   $ ")
			// Keep continuation lines of multi-line commands under the "$".
			sb.WriteString(strings.ReplaceAll(cmd.Command, "\n", "\n     "))
		}
	}
	return sb.String()
}

func init() {
	saveCmd.Flags().StringArrayVarP(&saveFlags.commands, "command", "c", nil, "shell command to attach")
	saveCmd.Flags().StringArrayVarP(&saveFlags.tags, "tag", "t", nil, "tag to assign")
	saveCmd.Flags().StringVarP(&saveFlags.description, "description", "d", "", "longer description of the memory")

	askCmd.Flags().IntVarP(&askFlags.limit, "limit", "n", 5, "maximum number of results to return")
	askCmd.Flags().Float64Var(&askFlags.minScore, "min-score", defaultMinScore, "hide results with a lower similarity (0-1); 0 shows everything")
	askCmd.Flags().BoolVarP(&askFlags.answer, "answer", "a", false, "synthesize an answer from the results with a local LLM")
	askCmd.Flags().StringVar(&askFlags.llm, "llm", "llama3.2", "Ollama model used by --answer")

	listCmd.Flags().StringVarP(&listFlags.tag, "tag", "t", "", "filter by tag")
	listCmd.Flags().IntVarP(&listFlags.limit, "limit", "n", 20, "maximum number of items to list")

	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(rememberCmd)
	rootCmd.AddCommand(reindexCmd)
}
