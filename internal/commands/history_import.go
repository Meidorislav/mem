package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/meidori/mem/internal/history"
	"github.com/meidori/mem/internal/llm"
	"github.com/meidori/mem/internal/storage"
)

// parseSelection reads "3, 7-9 12" style input for n numbered items: commas
// separate memories, spaces and ranges put several commands in one. It
// returns 0-based indices per memory, in the order given.
func parseSelection(input string, n int) ([][]int, error) {
	var groups [][]int
	for _, part := range strings.Split(input, ",") {
		var group []int
		seen := map[int]bool{}
		for _, tok := range strings.Fields(part) {
			lo, hi := tok, tok
			if a, b, ok := strings.Cut(tok, "-"); ok {
				lo, hi = a, b
			}
			from, err1 := strconv.Atoi(lo)
			to, err2 := strconv.Atoi(hi)
			if err1 != nil || err2 != nil || from < 1 || to > n || from > to {
				return nil, fmt.Errorf("invalid choice %q (use numbers 1-%d, ranges like 7-9, commas between memories)", tok, n)
			}
			for i := from; i <= to; i++ {
				if !seen[i] {
					seen[i] = true
					group = append(group, i-1)
				}
			}
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

// titlePrompt asks the LLM for a short title for cmds, in the style (and
// language) of titles the user already wrote.
func titlePrompt(cmds []string, examples []string) string {
	var sb strings.Builder
	sb.WriteString("Write a short title (3-8 words) saying what these shell commands are for, so the user can find them later.\n")
	if len(examples) > 0 {
		sb.WriteString("Match the language and style of the user's existing titles:\n")
		for _, e := range examples {
			fmt.Fprintf(&sb, "- %s\n", e)
		}
	}
	sb.WriteString("Reply with the title only, no quotes.\n\nCommands:\n")
	for _, c := range cmds {
		fmt.Fprintf(&sb, "$ %s\n", c)
	}
	return sb.String()
}

// cleanTitle keeps the first non-empty line of an LLM reply, without
// quotes, list markers or a "Title:" label.
func cleanTitle(reply string) string {
	for _, line := range strings.Split(reply, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*#> ")
		if l := strings.ToLower(line); strings.HasPrefix(l, "title:") {
			line = strings.TrimSpace(line[len("title:"):])
		}
		line = strings.Trim(line, "\"'`«»“” ")
		if line != "" {
			if r := []rune(line); len(r) > 80 {
				line = string(r[:80])
			}
			return line
		}
	}
	return ""
}

type titleSuggester interface {
	Generate(prompt string, w io.Writer) error
}

// runHistoryImport is `mem import --history`: pick commands from shell
// history, title them and save them as memories.
func runHistoryImport() error {
	if !stdinIsTerminal() {
		return errors.New("--history is interactive: run it in a terminal")
	}

	file := importFlags.file
	if file == "" {
		var err error
		if file, err = history.DefaultFile(); err != nil {
			return err
		}
	}
	all, err := history.Read(file)
	if err != nil {
		return err
	}
	recent := history.Recent(all, importFlags.last)
	if len(recent) == 0 {
		return fmt.Errorf("no commands worth importing in %s", file)
	}

	store, err := storage.NewStore()
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer store.Close()

	fmt.Printf("Last %d commands from %s:\n\n", len(recent), file)
	for i, c := range recent {
		fmt.Printf("%3d  %s\n", i+1, strings.ReplaceAll(c, "\n", "\n     "))
	}

	in := bufio.NewReader(os.Stdin)
	fmt.Print("\nPick commands to save: numbers or ranges, commas between memories (e.g. 3, 7-9). Enter to quit: ")
	line, _ := in.ReadString('\n')
	groups, err := parseSelection(line, len(recent))
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		fmt.Println("Nothing imported.")
		return nil
	}

	var suggester titleSuggester
	var examples []string
	if !importFlags.noSuggest {
		suggester = llm.NewClient(importFlags.llm)
		if prev, err := store.ListMemories("", 5); err == nil {
			for _, m := range prev {
				examples = append(examples, m.Title)
			}
		}
	}

	var saved []*storage.Memory
	for _, group := range groups {
		cmds := make([]string, len(group))
		for i, idx := range group {
			cmds[i] = recent[idx]
		}
		fmt.Println()
		for _, c := range cmds {
			fmt.Printf("  $ %s\n", strings.ReplaceAll(c, "\n", "\n    "))
		}

		suggestion := ""
		if suggester != nil {
			var reply strings.Builder
			if err := suggester.Generate(titlePrompt(cmds, examples), &reply); err != nil {
				fmt.Fprintf(os.Stderr, "Title suggestions off: %v\n", err)
				suggester = nil
			} else {
				suggestion = cleanTitle(reply.String())
			}
		}

		if suggestion != "" {
			fmt.Printf("Title [%s] (Enter = accept, - = skip): ", suggestion)
		} else {
			fmt.Print("Title (Enter or - = skip): ")
		}
		answer, _ := in.ReadString('\n')
		title := strings.TrimSpace(answer)
		if title == "" {
			title = suggestion
		}
		if title == "" || title == "-" {
			fmt.Println("Skipped.")
			continue
		}

		if dup, err := store.FindDuplicate(title, cmds); err != nil {
			return err
		} else if dup != 0 {
			fmt.Printf("Already saved as #%d, skipped.\n", dup)
			continue
		}

		m := &storage.Memory{Title: title, Source: storage.SourceRemember, Tags: importFlags.tags}
		for _, c := range cmds {
			m.Commands = append(m.Commands, storage.Command{Command: c})
		}
		if err := store.SaveMemory(m); err != nil {
			return fmt.Errorf("saving %q: %w", title, err)
		}
		fmt.Printf("Saved #%d: %s\n", m.ID, title)
		saved = append(saved, m)
	}

	fmt.Printf("\nImported %d %s from history.\n", len(saved), plural(len(saved), "memory", "memories"))
	if len(saved) > 0 {
		if err := indexAll(store, saved); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: imported memories are not searchable yet: %v\n", err)
			fmt.Fprintln(os.Stderr, "Run `mem reindex` once Ollama is available.")
		}
	}
	return nil
}
