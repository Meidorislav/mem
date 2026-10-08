package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/meidori/mem/internal/embeddings"
	"github.com/meidori/mem/internal/storage"
	"github.com/spf13/cobra"
)

// exportVersion is the format of `mem export` JSON. Bump it on incompatible
// changes and keep `mem import` reading the old versions.
const exportVersion = 1

type exportFile struct {
	Version    int              `json:"version"`
	ExportedAt time.Time        `json:"exported_at"`
	Memories   []exportedMemory `json:"memories"`
}

// exportedMemory is a memory without IDs or vectors: those belong to one
// database, and vectors are rebuilt by whichever model imports it.
type exportedMemory struct {
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Source      string            `json:"source"`
	Tags        []string          `json:"tags,omitempty"`
	Commands    []exportedCommand `json:"commands,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type exportedCommand struct {
	Command string  `json:"command"`
	Output  *string `json:"output,omitempty"`
}

var exportFlags struct {
	format string
	output string
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Write all memories to JSON (for backup) or Markdown (for reading)",
	Long: `Write every memory to stdout or a file. JSON keeps everything needed to
restore them with ` + "`mem import`" + `; Markdown is for reading.

  mem export -o ~/mem-backup.json
  mem export --format md -o memories.md`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if exportFlags.format != "json" && exportFlags.format != "md" {
			return fmt.Errorf("unknown format %q (use json or md)", exportFlags.format)
		}

		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		ids, err := store.AllMemoryIDs()
		if err != nil {
			return err
		}
		memories, err := store.GetMemoriesByIDs(ids)
		if err != nil {
			return err
		}

		w := io.Writer(os.Stdout)
		if exportFlags.output != "" && exportFlags.output != "-" {
			f, err := os.Create(exportFlags.output)
			if err != nil {
				return fmt.Errorf("creating %s: %w", exportFlags.output, err)
			}
			defer f.Close()
			w = f
		}

		if exportFlags.format == "md" {
			err = writeMarkdown(w, memories, time.Now())
		} else {
			err = writeJSON(w, memories, time.Now())
		}
		if err != nil {
			return err
		}
		if w != io.Writer(os.Stdout) {
			fmt.Fprintf(os.Stderr, "Exported %d %s to %s\n", len(memories), plural(len(memories), "memory", "memories"), exportFlags.output)
		}
		return nil
	},
}

func writeJSON(w io.Writer, memories []storage.Memory, now time.Time) error {
	file := exportFile{Version: exportVersion, ExportedAt: now.UTC(), Memories: []exportedMemory{}}
	for _, m := range memories {
		em := exportedMemory{
			Title:       m.Title,
			Description: m.Description,
			Source:      string(m.Source),
			Tags:        m.Tags,
			CreatedAt:   m.CreatedAt.UTC(),
			UpdatedAt:   m.UpdatedAt.UTC(),
		}
		for _, c := range m.Commands {
			em.Commands = append(em.Commands, exportedCommand{Command: c.Command, Output: c.Output})
		}
		file.Memories = append(file.Memories, em)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(file); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}
	return nil
}

func writeMarkdown(w io.Writer, memories []storage.Memory, now time.Time) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# mem export\n\n%d memories, exported %s.\n", len(memories), now.Format("2006-01-02"))
	for _, m := range memories {
		fmt.Fprintf(&sb, "\n## %s\n\n", m.Title)
		meta := []string{fmt.Sprintf("#%d", m.ID), m.CreatedAt.Format("2006-01-02"), string(m.Source)}
		if len(m.Tags) > 0 {
			meta = append(meta, "tags: "+strings.Join(m.Tags, ", "))
		}
		fmt.Fprintf(&sb, "_%s_\n", strings.Join(meta, " · "))
		if m.Description != "" {
			fmt.Fprintf(&sb, "\n%s\n", m.Description)
		}
		if len(m.Commands) > 0 {
			// A fence longer than any backtick run inside keeps commands intact.
			fence := "```"
			for _, c := range m.Commands {
				for strings.Contains(c.Command, fence) {
					fence += "`"
				}
			}
			fmt.Fprintf(&sb, "\n%ssh\n", fence)
			for _, c := range m.Commands {
				sb.WriteString(c.Command + "\n")
			}
			sb.WriteString(fence + "\n")
		}
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

var importFlags struct {
	force bool
}

var importCmd = &cobra.Command{
	Use:   "import <file.json|->",
	Short: "Restore memories from a `mem export` JSON file",
	Long: `Add the memories from a ` + "`mem export`" + ` JSON file (or - for stdin), keeping
their dates, then index them. Memories identical to one already stored
(same title and commands) are skipped unless --force.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var r io.Reader = os.Stdin
		if args[0] != "-" {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			r = f
		}
		memories, err := readExport(r)
		if err != nil {
			return err
		}

		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		var saved []*storage.Memory
		skipped := 0
		for _, m := range memories {
			if !importFlags.force {
				cmds := make([]string, len(m.Commands))
				for i, c := range m.Commands {
					cmds[i] = c.Command
				}
				dup, err := store.FindDuplicate(m.Title, cmds)
				if err != nil {
					return err
				}
				if dup != 0 {
					skipped++
					continue
				}
			}
			if err := store.SaveMemory(m); err != nil {
				return fmt.Errorf("saving %q after %d imported: %w", m.Title, len(saved), err)
			}
			saved = append(saved, m)
		}
		fmt.Printf("Imported %d %s", len(saved), plural(len(saved), "memory", "memories"))
		if skipped > 0 {
			fmt.Printf(", skipped %d already present", skipped)
		}
		fmt.Println(".")

		if len(saved) > 0 {
			if err := indexAll(store, saved); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: imported memories are not searchable yet: %v\n", err)
				fmt.Fprintln(os.Stderr, "Run `mem reindex` once Ollama is available.")
			}
		}
		return nil
	},
}

// readExport parses `mem export` JSON into memories ready to save.
func readExport(r io.Reader) ([]*storage.Memory, error) {
	var file exportFile
	dec := json.NewDecoder(r)
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("reading export: %w", err)
	}
	if file.Version < 1 || file.Version > exportVersion {
		return nil, fmt.Errorf("unsupported export version %d (this mem reads up to %d)", file.Version, exportVersion)
	}

	var out []*storage.Memory
	for i, em := range file.Memories {
		title := strings.TrimSpace(em.Title)
		if title == "" {
			return nil, fmt.Errorf("memory %d in the export has no title", i+1)
		}
		source := storage.MemorySource(em.Source)
		switch source {
		case storage.SourceSave, storage.SourceRemember, storage.SourceWatch:
		default:
			source = storage.SourceSave
		}
		m := &storage.Memory{
			Title:       title,
			Description: em.Description,
			Source:      source,
			Tags:        em.Tags,
			CreatedAt:   em.CreatedAt,
			UpdatedAt:   em.UpdatedAt,
		}
		for _, c := range em.Commands {
			if strings.TrimSpace(c.Command) == "" {
				continue
			}
			m.Commands = append(m.Commands, storage.Command{Command: c.Command, Output: c.Output})
		}
		out = append(out, m)
	}
	return out, nil
}

// indexAll embeds memories with the active model, opening the vector store
// once for the whole batch.
func indexAll(store *storage.Store, memories []*storage.Memory) error {
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
	for i, m := range memories {
		if _, err := indexMemory(context.Background(), store, emb, vecStore, cfg, m); err != nil {
			if i > 0 {
				return fmt.Errorf("after indexing %d: %w", i, err)
			}
			return err
		}
	}
	return nil
}

func init() {
	exportCmd.Flags().StringVar(&exportFlags.format, "format", "json", "json (for mem import) or md")
	exportCmd.Flags().StringVarP(&exportFlags.output, "output", "o", "", "file to write (default stdout)")
	importCmd.Flags().BoolVar(&importFlags.force, "force", false, "import memories even if an identical one exists")
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
}
