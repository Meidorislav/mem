// Package session records the commands typed in a `mem watch` shell so that
// `mem remember` can turn them into a memory.
//
// The watched shell appends every command line to a session file, each
// record terminated by a NUL byte so multi-line commands survive intact.
package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/meidori/mem/internal/paths"
	"mvdan.cc/sh/v3/syntax"
)

// EnvVar holds the session file path inside a watched shell.
const EnvVar = "MEM_SESSION"

const fileExt = ".session"

// ErrNoSession is returned when there is no recorded session to read.
var ErrNoSession = errors.New("no recorded session found; start one with `mem watch`")

// Dir returns the directory holding session files (~/.mem/sessions).
func Dir() (string, error) {
	app, err := paths.AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(app, "sessions"), nil
}

// Create makes a new empty session file in dir for the named shell and
// returns its path. The shell goes into the file name (see ShellOf).
func Create(dir, shell string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating session dir: %w", err)
	}
	name := time.Now().Format("20060102-150405.000000000")
	if shell != "" {
		name += "." + shell
	}
	path := filepath.Join(dir, name+fileExt)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("creating session file: %w", err)
	}
	return path, f.Close()
}

// ShellOf returns the shell a session file was recorded in, or "" for files
// from before the shell was part of the name.
func ShellOf(path string) string {
	ext := filepath.Ext(strings.TrimSuffix(filepath.Base(path), fileExt))
	switch shell := strings.TrimPrefix(ext, "."); shell {
	case "bash", "zsh", "fish":
		return shell
	}
	return ""
}

// Latest returns the most recent session file in dir.
func Latest(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*"+fileExt))
	if err != nil {
		return "", fmt.Errorf("listing sessions: %w", err)
	}
	if len(matches) == 0 {
		return "", ErrNoSession
	}
	// File names are timestamps, so lexical order is chronological.
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// Entry is a recorded command with the output it produced, if captured.
type Entry struct {
	Command string
	Output  string
}

// Read returns the raw command lines recorded in a session file.
func Read(path string) ([]string, error) {
	entries, err := ReadEntries(path)
	if err != nil {
		return nil, err
	}
	cmds := make([]string, len(entries))
	for i, e := range entries {
		cmds[i] = e.Command
	}
	return cmds, nil
}

// ReadEntries returns the raw records of a session file. An output record
// belongs to the command record before it; one with no command before it
// (the file was truncated while the command ran) is dropped.
func ReadEntries(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}

	var entries []Entry
	for _, rec := range bytes.Split(data, []byte{0}) {
		switch {
		case len(rec) == 0:
		case rec[0] == recordMark:
			if n := len(entries); n > 0 {
				entries[n-1].Output = string(rec[1:])
			}
		default:
			entries = append(entries, Entry{Command: string(rec)})
		}
	}
	return entries, nil
}

// Clean drops noise from command lines recorded in shell: blanks, mem's
// own invocations, shell exits and consecutive duplicates. Multi-line
// records (e.g. a pasted block that zsh or fish runs as one line) are split
// into their separate commands first.
func Clean(cmds []string, shell string) []string {
	entries := make([]Entry, len(cmds))
	for i, c := range cmds {
		entries[i].Command = c
	}
	entries = CleanEntries(entries, shell)
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Command
	}
	return out
}

// CleanEntries is Clean for entries. When a record is split, its output goes
// to the last command; of consecutive duplicates the latest run's output wins.
func CleanEntries(entries []Entry, shell string) []Entry {
	split := splitLines
	if shell == "fish" {
		split = splitFish
	}
	var out []Entry
	for _, rec := range entries {
		parts := split(rec.Command)
		for i, c := range parts {
			e := Entry{Command: strings.TrimSpace(c)}
			if i == len(parts)-1 {
				e.Output = rec.Output
			}
			if e.Command == "" || isNoise(e.Command) {
				continue
			}
			if n := len(out); n > 0 && out[n-1].Command == e.Command {
				out[n-1].Output = e.Output
				continue
			}
			out = append(out, e)
		}
	}
	return out
}

// splitLines splits a multi-line record into the commands written on
// separate lines, keeping multi-line constructs (loops, pipelines continued
// with \, heredocs) intact. Commands sharing a line stay together. Records
// that do not parse as shell are returned unchanged.
func splitLines(rec string) []string {
	if !strings.Contains(rec, "\n") {
		return []string{rec}
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(rec), "")
	if err != nil || len(file.Stmts) == 0 {
		return []string{rec}
	}

	// Group statements by the line they start on; statements sharing a line
	// stay one command. A group ends at its last statement, or at the end of
	// a heredoc body, which the statement's own range does not cover.
	type span struct{ start, end, line uint }
	var groups []span
	for _, st := range file.Stmts {
		end := stmtEnd(st)
		if n := len(groups); n > 0 && st.Pos().Line() == groups[n-1].line {
			groups[n-1].end = max(groups[n-1].end, end)
			groups[n-1].line = st.End().Line()
			continue
		}
		groups = append(groups, span{st.Pos().Offset(), end, st.End().Line()})
	}

	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, rec[g.start:min(g.end, uint(len(rec)))])
	}
	return out
}

// stmtEnd returns the offset just past st, including any heredoc bodies.
func stmtEnd(st *syntax.Stmt) uint {
	end := st.End().Offset()
	syntax.Walk(st, func(node syntax.Node) bool {
		if r, ok := node.(*syntax.Redirect); ok && r.Hdoc != nil {
			end = max(end, r.Hdoc.End().Offset())
		}
		return true
	})
	return end
}

func isNoise(cmd string) bool {
	fields := strings.Fields(cmd)
	switch filepath.Base(fields[0]) {
	case "mem", "exit", "logout":
		return true
	}
	return false
}

// Shell describes how to start a watched interactive shell.
type Shell struct {
	Name string
	cmd  *exec.Cmd
	tmp  string
}

// Run runs the shell on the current terminal, without capturing output.
func (s *Shell) Run() error {
	s.cmd.Stdin, s.cmd.Stdout, s.cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return s.cmd.Run()
}

// Cleanup removes the temporary startup files.
func (s *Shell) Cleanup() { os.RemoveAll(s.tmp) }

// ResolveShell finds the shell to watch: shellPath, else $SHELL, else bash.
// It returns the executable and its name (bash, zsh or fish).
func ResolveShell(shellPath string) (path, name string, err error) {
	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
	}
	if shellPath == "" {
		shellPath = "bash"
	}
	path, err = exec.LookPath(shellPath)
	if err != nil {
		return "", "", fmt.Errorf("finding shell %q: %w", shellPath, err)
	}
	switch name = filepath.Base(path); name {
	case "bash", "zsh", "fish":
		return path, name, nil
	}
	return "", "", fmt.Errorf("unsupported shell %q (supported: bash, zsh, fish); pick one with --shell", name)
}

// NewShell prepares an interactive shell that records its commands into
// sessionPath. shellPath may be empty to use $SHELL.
func NewShell(shellPath, sessionPath string) (*Shell, error) {
	resolved, name, err := ResolveShell(shellPath)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "mem-watch-")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	env := append(os.Environ(), EnvVar+"="+sessionPath)

	var cmd *exec.Cmd
	switch name {
	case "bash":
		rc := filepath.Join(tmp, "bashrc")
		if err := os.WriteFile(rc, []byte(bashRC), 0o600); err != nil {
			os.RemoveAll(tmp)
			return nil, fmt.Errorf("writing bashrc: %w", err)
		}
		cmd = exec.Command(resolved, "--rcfile", rc, "-i")
	case "zsh":
		if err := writeZshStartup(tmp); err != nil {
			os.RemoveAll(tmp)
			return nil, err
		}
		orig := os.Getenv("ZDOTDIR")
		if orig == "" {
			orig = os.Getenv("HOME")
		}
		env = append(env, "MEM_ORIG_ZDOTDIR="+orig, "ZDOTDIR="+tmp)
		cmd = exec.Command(resolved, "-i")
	case "fish":
		// -C runs after the user's config.fish, before the first prompt.
		cmd = exec.Command(resolved, "-i", "-C", fishRC)
	}

	cmd.Env = env
	return &Shell{Name: name, cmd: cmd, tmp: tmp}, nil
}

func writeZshStartup(dir string) error {
	files := map[string]string{".zshenv": zshEnv, ".zshrc": zshRC}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}

// bashRC loads the user's config, then logs each executed history entry from
// PROMPT_COMMAND. Comparing `history 1` skips re-logging when the user just
// presses Enter.
const bashRC = `[ -f ~/.bashrc ] && . ~/.bashrc
# Markers tell mem's output recorder where a command's output begins and
# ends (see capture.go). PS0 (bash 4.4+) prints right before a command runs;
# older bash just records no output.
__mem_mark() { [ -n "$MEM_CAPTURE" ] && printf '\033]777;mem;%s\007' "$1"; return 0; }
[ -n "$MEM_CAPTURE" ] && PS0=$'\033]777;mem;start\007'"${PS0-}"
__mem_record() {
  local entry cmd
  entry="$(HISTTIMEFORMAT= builtin history 1)"
  # bash loads the history file after rc files, so the first call only
  # remembers where the existing history ends.
  if [ -z "${__mem_ready+x}" ]; then
    __mem_ready=1
    __mem_last="$entry"
    return
  fi
  if [ -z "$entry" ] || [ "$entry" = "$__mem_last" ]; then
    __mem_mark skip
    return
  fi
  __mem_last="$entry"
  # Strip the "  123* " history number prefix.
  cmd="${entry#"${entry%%[![:space:]]*}"}"
  cmd="${cmd#"${cmd%%[![:digit:]]*}"}"
  cmd="${cmd#\*}"
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  if [ -n "$cmd" ]; then
    printf '%s\0' "$cmd" >> "$MEM_SESSION"
    __mem_mark end
  else
    __mem_mark skip
  fi
}
PROMPT_COMMAND="__mem_record${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
PS1="(mem) $PS1"
`

// zshEnv loads the user's .zshenv but keeps ZDOTDIR pointing at our temp
// dir so zsh picks up the .zshrc below. If the user's .zshenv sets ZDOTDIR
// itself, that becomes the directory whose .zshrc we load.
const zshEnv = `__mem_tmp="$ZDOTDIR"
[ -f "$MEM_ORIG_ZDOTDIR/.zshenv" ] && . "$MEM_ORIG_ZDOTDIR/.zshenv"
[ "$ZDOTDIR" != "$__mem_tmp" ] && MEM_ORIG_ZDOTDIR="${ZDOTDIR:-$HOME}"
ZDOTDIR="$__mem_tmp"
unset __mem_tmp
`

// zshRC restores the user's ZDOTDIR, loads their .zshrc and logs each command
// line from a preexec hook.
const zshRC = `ZDOTDIR="$MEM_ORIG_ZDOTDIR"
[ -f "$ZDOTDIR/.zshrc" ] && . "$ZDOTDIR/.zshrc"
[ "$ZDOTDIR" = "$HOME" ] && unset ZDOTDIR
__mem_record() {
  # Match bash: honour HIST_IGNORE_SPACE so " cmd" stays unrecorded.
  [[ -o histignorespace && "$1" == " "* ]] && return
  printf '%s\0' "$1" >> "$MEM_SESSION"
  # Markers around the output, see capture.go; skipped commands print none.
  [[ -n "$MEM_CAPTURE" ]] && printf '\033]777;mem;start\007'
}
__mem_precmd() {
  [[ -n "$MEM_CAPTURE" ]] && printf '\033]777;mem;end\007'
}
autoload -Uz add-zsh-hook
add-zsh-hook preexec __mem_record
add-zsh-hook precmd __mem_precmd
PROMPT="(mem) $PROMPT"
`

// fishRC logs each command line from the fish_preexec event, skipping
// space-prefixed ones like fish's own history does, and prefixes the
// prompt while keeping $status for prompts that show it.
const fishRC = `function __mem_record --on-event fish_preexec
    string match -q -- ' *' $argv[1]; and return
    string join0 -- $argv[1] >> $MEM_SESSION
    # Markers around the output, see capture.go; skipped commands print none.
    set -q MEM_CAPTURE; and printf '\033]777;mem;start\007'
end
function __mem_done --on-event fish_postexec
    set -q MEM_CAPTURE; and printf '\033]777;mem;end\007'
end
if functions -q fish_prompt
    functions -c fish_prompt __mem_orig_prompt
    function __mem_status
        return $argv[1]
    end
    function fish_prompt
        set -l s $status
        printf '(mem) '
        __mem_status $s
        __mem_orig_prompt
    end
end
`
