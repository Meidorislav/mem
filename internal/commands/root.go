package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
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
	force       bool
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

		if !saveFlags.force {
			if err := checkDuplicate(store, m); err != nil {
				return err
			}
		}

		if err := store.SaveMemory(m); err != nil {
			return fmt.Errorf("saving memory: %w", err)
		}

		fmt.Printf("Saved #%d: %s\n", m.ID, title)
		warnIfNotIndexed(store, m)
		return nil
	},
}

// checkDuplicate refuses to save an exact copy (same title and commands) of
// an existing memory, which would only clutter search results.
func checkDuplicate(store *storage.Store, m *storage.Memory) error {
	cmds := make([]string, len(m.Commands))
	for i, c := range m.Commands {
		cmds[i] = c.Command
	}
	id, err := store.FindDuplicate(m.Title, cmds)
	if err != nil {
		return err
	}
	if id != 0 {
		return fmt.Errorf("already saved as #%d %q: change it with `mem edit %d`, or pass --force to save a copy", id, m.Title, id)
	}
	return nil
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

		question := strings.Join(args, " ")

		limit := askFlags.limit
		if limit <= 0 {
			limit = 5
		}

		keywordIDs, err := store.KeywordSearch(question, limit)
		if err != nil {
			return err
		}

		hits, model, semErr := semanticSearch(store, question, limit)
		if semErr != nil {
			// Keyword search needs no Ollama, so it can still answer.
			if len(keywordIDs) == 0 {
				return semErr
			}
			fmt.Fprintf(os.Stderr, "Warning: semantic search unavailable, showing keyword matches only: %v\n", semErr)
		}

		minScore := askFlags.minScore
		if !cmd.Flags().Changed("min-score") {
			minScore = embeddings.MinScore(model)
		}

		results := fuse(rankHits(hits, minScore, limit), hits, keywordIDs, limit)
		if len(results) == 0 {
			if len(hits) == 0 {
				fmt.Println("No matching memories found.")
			} else {
				fmt.Printf("No memories scored above %.2f (best: %.2f). Try a lower --min-score.\n", minScore, hits[0].Score)
			}
			return nil
		}

		ids := make([]int64, len(results))
		notes := make(map[int64]string, len(results))
		for i, r := range results {
			ids[i] = r.MemoryID
			notes[r.MemoryID] = resultNote(r)
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
				return fmt.Errorf("generating answer with %s: %w", askFlags.llm, err)
			}
			fmt.Print("\n\nSources:\n")
		}

		fmt.Println(formatSearchResults(memories, notes))
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
	Use:     "delete <id>...",
	Aliases: []string{"rm", "remove"},
	Short:   "Delete memories by ID",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ids := make([]int64, len(args))
		for i, a := range args {
			id, err := strconv.ParseInt(a, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid memory ID: %s", a)
			}
			ids[i] = id
		}

		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		// SQLite is the source of truth; a leftover vector is harmless since
		// search results are joined back to SQLite and unknown IDs skipped.
		var vecStore *vector.Store
		if cfg, err := store.GetActiveEmbeddingConfig(); err == nil && cfg != nil {
			if vs, err := vector.NewStore(cfg.Dimensions); err == nil {
				vecStore = vs
				defer vecStore.Close()
			}
		}

		failed := 0
		for _, id := range ids {
			if err := store.DeleteMemory(id); err != nil {
				fmt.Fprintf(os.Stderr, "#%d: %v\n", id, err)
				failed++
				continue
			}
			if vecStore != nil {
				_ = vecStore.Delete(context.Background(), id)
			}
			fmt.Printf("Deleted memory #%d\n", id)
		}
		if failed > 0 {
			return fmt.Errorf("%d of %d memories not deleted", failed, len(ids))
		}
		return nil
	},
}

// semanticSearch embeds question with the active model and returns the
// nearest chunks, best first, and the model that scored them.
func semanticSearch(store *storage.Store, question string, limit int) ([]vector.Hit, string, error) {
	cfg, err := activeConfig(store)
	if err != nil {
		return nil, "", err
	}

	vecStore, err := openVectors(cfg)
	if err != nil {
		return nil, "", err
	}
	defer vecStore.Close()

	queryVec, err := embeddings.NewClient(cfg.ModelName).EmbedQuery(question)
	if err != nil {
		return nil, "", fmt.Errorf("embedding the question with %s: %w", cfg.ModelName, err)
	}

	// Multi-chunk memories return one hit per chunk, so over-fetch
	// before deduplicating by memory ID.
	hits, err := vecStore.Search(context.Background(), queryVec, max(limit*3, 10))
	if err != nil {
		return nil, "", fmt.Errorf("searching vector store: %w", err)
	}

	if n, err := store.CountUnindexedMemories(cfg.ID); err == nil && n > 0 {
		fmt.Fprintf(os.Stderr, "Note: %d %s not indexed with the current settings; run `mem reindex` to fix that.\n", n, plural(n, "memory is", "memories are"))
	}
	return hits, cfg.ModelName, nil
}

// resultNote is the annotation shown next to a result: its similarity and
// whether the keyword index matched it.
func resultNote(r result) string {
	switch {
	case r.Score >= 0 && r.Keyword:
		return fmt.Sprintf("%.2f, keyword", r.Score)
	case r.Keyword:
		return "keyword"
	default:
		return fmt.Sprintf("%.2f", r.Score)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// formatSearchResults renders memories as a numbered list. notes, when
// non-nil, adds an annotation such as the similarity to the query.
func formatSearchResults(memories []storage.Memory, notes map[int64]string) string {
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
		if note, ok := notes[m.ID]; ok {
			sb.WriteString(fmt.Sprintf("  (%s)", note))
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
	saveCmd.Flags().BoolVar(&saveFlags.force, "force", false, "save even if an identical memory exists")

	askCmd.Flags().IntVarP(&askFlags.limit, "limit", "n", 5, "maximum number of results to return")
	askCmd.Flags().Float64Var(&askFlags.minScore, "min-score", 0, "hide results with a lower similarity (0-1); 0 shows everything (default: tuned per embedding model, 0.45 for bge-m3)")
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
