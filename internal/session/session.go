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

// Create makes a new empty session file in dir and returns its path.
func Create(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating session dir: %w", err)
	}
	name := time.Now().Format("20060102-150405.000000000") + fileExt
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("creating session file: %w", err)
	}
	return path, f.Close()
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

// Read returns the raw command lines recorded in a session file.
func Read(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}

	var cmds []string
	for _, rec := range bytes.Split(data, []byte{0}) {
		if len(rec) > 0 {
			cmds = append(cmds, string(rec))
		}
	}
	return cmds, nil
}

// Clean drops noise from recorded command lines: blanks, mem's own
// invocations, shell exits and consecutive duplicates. Multi-line records
// (e.g. a pasted block that zsh runs as one line) are split into their
// separate commands first.
func Clean(cmds []string) []string {
	var out []string
	for _, rec := range cmds {
		for _, c := range splitLines(rec) {
			c = strings.TrimSpace(c)
			if c == "" || isNoise(c) {
				continue
			}
			if len(out) > 0 && out[len(out)-1] == c {
				continue
			}
			out = append(out, c)
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

// Cmd returns the command that starts the shell.
func (s *Shell) Cmd() *exec.Cmd { return s.cmd }

// Cleanup removes the temporary startup files.
func (s *Shell) Cleanup() { os.RemoveAll(s.tmp) }

// NewShell prepares an interactive shell that records its commands into
// sessionPath. shellPath may be empty to use $SHELL.
func NewShell(shellPath, sessionPath string) (*Shell, error) {
	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
	}
	if shellPath == "" {
		shellPath = "bash"
	}
	resolved, err := exec.LookPath(shellPath)
	if err != nil {
		return nil, fmt.Errorf("finding shell %q: %w", shellPath, err)
	}

	tmp, err := os.MkdirTemp("", "mem-watch-")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	name := filepath.Base(resolved)
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
	default:
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("unsupported shell %q (supported: bash, zsh); pick one with --shell", name)
	}

	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
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
  [ -z "$entry" ] || [ "$entry" = "$__mem_last" ] && return
  __mem_last="$entry"
  # Strip the "  123* " history number prefix.
  cmd="${entry#"${entry%%[![:space:]]*}"}"
  cmd="${cmd#"${cmd%%[![:digit:]]*}"}"
  cmd="${cmd#\*}"
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  [ -n "$cmd" ] && printf '%s\0' "$cmd" >> "$MEM_SESSION"
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
}
autoload -Uz add-zsh-hook
add-zsh-hook preexec __mem_record
PROMPT="(mem) $PROMPT"
`
