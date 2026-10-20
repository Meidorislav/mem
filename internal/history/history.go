// Package history reads shell history files (zsh, bash and fish) for
// `mem import --history`.
package history

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultFile picks the history file to read: $HISTFILE when exported,
// else the default file of the user's $SHELL, else whichever of
// ~/.zsh_history, ~/.bash_history and fish's history exists.
func DefaultFile() (string, error) {
	if f := os.Getenv("HISTFILE"); f != "" {
		return f, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	zsh := filepath.Join(home, ".zsh_history")
	bash := filepath.Join(home, ".bash_history")
	fish := fishFile(home)
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return zsh, nil
	case "bash":
		return bash, nil
	case "fish":
		return fish, nil
	}
	for _, f := range []string{zsh, bash, fish} {
		if _, err := os.Stat(f); err == nil {
			return f, nil
		}
	}
	return "", fmt.Errorf("no shell history found (looked for %s, %s and %s); pass --file", zsh, bash, fish)
}

// fishFile is where fish keeps its history: $XDG_DATA_HOME/fish, else
// ~/.local/share/fish.
func fishFile(home string) string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "fish", "fish_history")
}

// Read returns the commands in a history file, oldest first.
func Read(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading history: %w", err)
	}
	return Parse(data), nil
}

// zshMeta marks a metafied byte in zsh history: the next byte is the
// original XOR 0x20. zsh does this for bytes 0x83-0xa2, which UTF-8 uses
// for Cyrillic letters such as у, ф, х.
const zshMeta = 0x83

// Parse reads zsh (plain or extended `: <time>:<duration>;cmd`), bash
// (with optional `#<time>` lines) and fish history. In zsh files a line
// ending with a backslash continues on the next line.
func Parse(data []byte) []string {
	if isFish(data) {
		return parseFish(data)
	}
	data = unmetafy(data)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	var cmds []string
	var cur []string
	flush := func() {
		if cur != nil {
			if c := strings.TrimSpace(strings.Join(cur, "\n")); c != "" {
				cmds = append(cmds, c)
			}
			cur = nil
		}
	}
	for _, line := range lines {
		if cur == nil {
			if isBashTimestamp(line) {
				continue
			}
			line = stripZshHeader(line)
		}
		if strings.HasSuffix(line, `\`) && !strings.HasSuffix(line, `\\`) {
			cur = append(cur, strings.TrimSuffix(line, `\`))
			continue
		}
		cur = append(cur, line)
		flush()
	}
	flush()
	return cmds
}

// fishCmd starts an entry in fish's YAML-like history; `when:` and
// `paths:` lines follow it.
const fishCmd = "- cmd: "

func isFish(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(data, "\n"), []byte(fishCmd))
}

func parseFish(data []byte) []string {
	var cmds []string
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, fishCmd); ok {
			if c := strings.TrimSpace(unescapeFish(rest)); c != "" {
				cmds = append(cmds, c)
			}
		}
	}
	return cmds
}

// unescapeFish undoes fish's history escaping: `\\` for a backslash and
// `\n` for a newline.
func unescapeFish(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\':
				sb.WriteByte('\\')
				i++
				continue
			case 'n':
				sb.WriteByte('\n')
				i++
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func unmetafy(data []byte) []byte {
	if bytes.IndexByte(data, zshMeta) < 0 {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == zshMeta && i+1 < len(data) {
			i++
			out = append(out, data[i]^0x20)
			continue
		}
		out = append(out, data[i])
	}
	return out
}

// stripZshHeader removes the `: 1696000000:0;` prefix of extended history.
func stripZshHeader(line string) string {
	if !strings.HasPrefix(line, ": ") {
		return line
	}
	head, rest, ok := strings.Cut(line, ";")
	if !ok {
		return line
	}
	ts, dur, ok := strings.Cut(strings.TrimPrefix(head, ": "), ":")
	if !ok || !allDigits(ts) || !allDigits(dur) {
		return line
	}
	return rest
}

func isBashTimestamp(line string) bool {
	return len(line) > 1 && line[0] == '#' && allDigits(line[1:])
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// trivial commands are not worth remembering without arguments.
var trivial = map[string]bool{
	"ls": true, "ll": true, "la": true, "l": true, "pwd": true, "clear": true,
	"exit": true, "history": true, "cd": true, "whoami": true, "date": true,
	"top": true, "htop": true, "vim": true, "nvim": true, "nano": true,
	"git": true, "fg": true, "bg": true, "jobs": true, "reset": true,
}

// Interesting drops noise: trivial commands without arguments, cd
// anywhere, mem's own invocations and very short lines.
func Interesting(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 || len(cmd) < 4 {
		return false
	}
	switch name := filepath.Base(fields[0]); {
	case name == "mem", name == "cd":
		return false
	case len(fields) == 1 && trivial[name]:
		return false
	case name == "git" && len(fields) == 2 && (fields[1] == "status" || fields[1] == "diff" || fields[1] == "log" || fields[1] == "pull" || fields[1] == "push"):
		return false
	}
	return true
}

// Recent returns up to n interesting commands from cmds (oldest first),
// most recent last, each kept only at its latest occurrence.
func Recent(cmds []string, n int) []string {
	seen := map[string]bool{}
	var picked []string
	for i := len(cmds) - 1; i >= 0 && len(picked) < n; i-- {
		c := cmds[i]
		if seen[c] || !Interesting(c) {
			continue
		}
		seen[c] = true
		picked = append(picked, c)
	}
	for i, j := 0, len(picked)-1; i < j; i, j = i+1, j-1 {
		picked[i], picked[j] = picked[j], picked[i]
	}
	return picked
}
