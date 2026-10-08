package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/meidori/mem/internal/storage"
	"golang.org/x/term"
)

// errCancelled means the user declined at a prompt; it is not a failure.
var errCancelled = errors.New("cancelled")

// errNoMatch is returned by `ask --print`, which has no other way to say
// that nothing was found.
var errNoMatch = errors.New("no matching memories")

// stdinIsTerminal reports whether someone can answer prompts. A plain
// character-device check is not enough: /dev/null is one too.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// chooseCommands picks commands from m: the only one, or the user's choice
// when there are several. Without an interactive stdin it takes them all.
func chooseCommands(m storage.Memory, in *bufio.Reader, out io.Writer, interactive bool) ([]string, error) {
	cmds := make([]string, len(m.Commands))
	for i, c := range m.Commands {
		cmds[i] = c.Command
	}
	switch {
	case len(cmds) == 0:
		return nil, fmt.Errorf("#%d %q has no commands", m.ID, m.Title)
	case len(cmds) == 1 || !interactive:
		return cmds, nil
	}

	fmt.Fprintf(out, "\n#%d %s:\n", m.ID, m.Title)
	for i, c := range cmds {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, strings.ReplaceAll(c, "\n", "\n      "))
	}
	fmt.Fprintf(out, "Which command? (1-%d, a = all, Enter = cancel): ", len(cmds))

	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return nil, errCancelled
	}
	switch answer := strings.ToLower(strings.TrimSpace(line)); answer {
	case "":
		return nil, errCancelled
	case "a", "all":
		return cmds, nil
	default:
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(cmds) {
			return nil, fmt.Errorf("invalid choice %q", answer)
		}
		return cmds[n-1 : n], nil
	}
}

// clipboardTools are tried in order; the first one installed wins.
var clipboardTools = [][]string{
	{"pbcopy"},
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
	{"clip.exe"},
}

// copyToClipboard puts text on the system clipboard.
func copyToClipboard(text string) error {
	for _, tool := range clipboardTools {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w: %s", tool[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return errors.New("no clipboard tool found (pbcopy, wl-copy, xclip, xsel)")
}

// confirm asks a yes/no question; anything but y/yes is no.
func confirm(in *bufio.Reader, out io.Writer, question string) bool {
	fmt.Fprintf(out, "%s [y/N]: ", question)
	line, _ := in.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "д", "да":
		return true
	}
	return false
}

// runCommands runs cmds one by one through shell in the current directory,
// stopping at the first failure.
func runCommands(cmds []string, shell string, stdin io.Reader, stdout, stderr io.Writer) error {
	for _, c := range cmds {
		cmd := exec.Command(shell, "-c", c)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return fmt.Errorf("%q exited with status %d", c, exitErr.ExitCode())
			}
			return fmt.Errorf("running %q: %w", c, err)
		}
	}
	return nil
}

// userShell is the shell commands are run with: $SHELL, else sh.
func userShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "sh"
}

// useResult copies and/or runs commands from the top search result, as
// requested by --copy and --run.
func useResult(m storage.Memory, copyIt, runIt bool) error {
	interactive := stdinIsTerminal()
	if runIt && !interactive {
		return errors.New("--run needs an interactive terminal to confirm the command")
	}

	in := bufio.NewReader(os.Stdin)
	cmds, err := chooseCommands(m, in, os.Stdout, interactive)
	if errors.Is(err, errCancelled) {
		fmt.Println("Cancelled.")
		return nil
	}
	if err != nil {
		return err
	}

	if copyIt {
		if err := copyToClipboard(strings.Join(cmds, "\n")); err != nil {
			return fmt.Errorf("copying to clipboard: %w", err)
		}
		fmt.Printf("Copied %d %s to the clipboard.\n", len(cmds), plural(len(cmds), "command", "commands"))
	}

	if runIt {
		fmt.Println()
		for _, c := range cmds {
			fmt.Printf("  $ %s\n", strings.ReplaceAll(c, "\n", "\n    "))
		}
		question := "Run this command?"
		if len(cmds) > 1 {
			question = fmt.Sprintf("Run these %d commands?", len(cmds))
		}
		if !confirm(in, os.Stdout, question) {
			fmt.Println("Not run.")
			return nil
		}
		return runCommands(cmds, userShell(), os.Stdin, os.Stdout, os.Stderr)
	}
	return nil
}

// printResult writes the top result's command(s) to stdout and nothing
// else, for `ask --print`: the shell widget puts stdout on the command
// line, while prompts go to stderr, i.e. the terminal.
func printResult(m storage.Memory) error {
	cmds, err := chooseCommands(m, bufio.NewReader(os.Stdin), os.Stderr, stdinIsTerminal())
	if err != nil {
		return err
	}
	fmt.Println(strings.Join(cmds, "\n"))
	return nil
}
