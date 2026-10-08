package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/meidori/mem/internal/storage"
	"github.com/spf13/cobra"
)

var editFlags struct {
	title       string
	description string
	commands    []string
	tags        []string
}

var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Change a memory's title, description, commands or tags",
	Long: `Edit a memory in $EDITOR, or change single fields with flags:

  mem edit 3                        # opens $VISUAL / $EDITOR (default vi)
  mem edit 3 --title "new title"
  mem edit 3 -c "ls -lhS" -c "du -sh *"   # replaces all commands
  mem edit 3 -t files -t disk             # replaces all tags

The memory is re-indexed after the change.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
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

		flags := cmd.Flags()
		if flags.Changed("title") || flags.Changed("description") || flags.Changed("command") || flags.Changed("tag") {
			if flags.Changed("title") {
				m.Title = strings.TrimSpace(editFlags.title)
			}
			if flags.Changed("description") {
				m.Description = strings.TrimSpace(editFlags.description)
			}
			if flags.Changed("command") {
				m.Commands = nil
				for _, c := range editFlags.commands {
					m.Commands = append(m.Commands, storage.Command{Command: c})
				}
			}
			if flags.Changed("tag") {
				m.Tags = editFlags.tags
			}
		} else {
			edited, err := editInEditor(m)
			if err != nil {
				return err
			}
			if edited == nil {
				fmt.Println("No changes.")
				return nil
			}
			m = edited
		}

		if m.Title == "" {
			return errors.New("title cannot be empty")
		}
		if err := store.UpdateMemory(m); err != nil {
			return fmt.Errorf("updating memory: %w", err)
		}

		fmt.Printf("Updated #%d: %s\n", m.ID, m.Title)
		warnIfNotIndexed(store, m)
		return nil
	},
}

// editInEditor opens m in the user's editor and returns the edited memory,
// or nil if the text was not changed.
func editInEditor(m *storage.Memory) (*storage.Memory, error) {
	f, err := os.CreateTemp("", fmt.Sprintf("mem-%d-*.txt", m.ID))
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(f.Name())

	original := formatForEdit(m)
	if _, err := f.WriteString(original); err != nil {
		f.Close()
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// Through sh so EDITOR may carry arguments, e.g. "code --wait".
	run := exec.Command("sh", "-c", editor+` "$1"`, "mem-edit", f.Name())
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := run.Run(); err != nil {
		return nil, fmt.Errorf("running editor %q: %w", editor, err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		return nil, fmt.Errorf("reading edited file: %w", err)
	}
	if string(data) == original {
		return nil, nil
	}

	edited, err := parseEdit(string(data))
	if err != nil {
		return nil, err
	}
	edited.ID = m.ID
	return edited, nil
}

const editHelp = `# Edit memory #%d. Lines starting with '#' above the commands are ignored.
# Each command starts with "$ "; indented lines below it continue it.
# Save and quit to apply; quit without saving to cancel.

`

// formatForEdit renders m as the text shown in the editor.
func formatForEdit(m *storage.Memory) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, editHelp, m.ID)
	fmt.Fprintf(&sb, "title: %s\n", m.Title)
	fmt.Fprintf(&sb, "tags: %s\n", strings.Join(m.Tags, ", "))
	fmt.Fprintf(&sb, "description: %s\n\n", m.Description)
	for _, c := range m.Commands {
		lines := strings.Split(c.Command, "\n")
		fmt.Fprintf(&sb, "$ %s\n", lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(&sb, "  %s\n", l)
		}
	}
	return sb.String()
}

// parseEdit reads the editor text back into a memory (without an ID).
func parseEdit(text string) (*storage.Memory, error) {
	m := &storage.Memory{}
	var current []string
	flush := func() {
		if current != nil {
			cmd := strings.TrimRight(strings.Join(current, "\n"), " \t\n")
			if strings.TrimSpace(cmd) != "" {
				m.Commands = append(m.Commands, storage.Command{Command: cmd})
			}
		}
		current = nil
	}

	inCommands := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "$ ") || line == "$" {
			flush()
			inCommands = true
			current = []string{strings.TrimPrefix(strings.TrimPrefix(line, "$"), " ")}
			continue
		}
		if inCommands {
			current = append(current, strings.TrimPrefix(line, "  "))
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("unexpected line %q: expected title:, tags:, description: or a $ command", trimmed)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "title":
			m.Title = value
		case "description":
			m.Description = value
		case "tags":
			for _, t := range strings.Split(value, ",") {
				if t = strings.TrimSpace(t); t != "" {
					m.Tags = append(m.Tags, t)
				}
			}
		default:
			return nil, fmt.Errorf("unknown field %q: expected title, tags or description", key)
		}
	}
	flush()
	return m, nil
}

func init() {
	editCmd.Flags().StringVar(&editFlags.title, "title", "", "new title")
	editCmd.Flags().StringVarP(&editFlags.description, "description", "d", "", "new description")
	editCmd.Flags().StringArrayVarP(&editFlags.commands, "command", "c", nil, "command to keep (replaces all commands)")
	editCmd.Flags().StringArrayVarP(&editFlags.tags, "tag", "t", nil, "tag to keep (replaces all tags)")
	rootCmd.AddCommand(editCmd)
}
