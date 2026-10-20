package session

import "strings"

// splitFish splits a multi-line fish record into the commands written on
// separate lines. fish syntax differs from bash (blocks close with `end`,
// `and`/`or` chain on the next line), so mvdan.cc/sh can't parse it; this
// tracks just enough to keep blocks, quotes, command substitutions, `\`
// continuations and trailing pipes together.
func splitFish(rec string) []string {
	var groups []string
	var cur []string
	var st fishState
	for _, line := range strings.Split(rec, "\n") {
		if len(cur) == 0 && strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if len(cur) == 0 && len(groups) > 0 && chainsPrevious(line) {
			cur = []string{groups[len(groups)-1]}
			groups = groups[:len(groups)-1]
		}
		cur = append(cur, line)
		st.scanLine(line)
		if st.complete() {
			if g := strings.TrimSpace(strings.Join(cur, "\n")); g != "" {
				groups = append(groups, g)
			}
			cur = nil
		}
	}
	if g := strings.TrimSpace(strings.Join(cur, "\n")); g != "" {
		groups = append(groups, g)
	}
	return groups
}

// chainsPrevious reports whether a line only makes sense after the previous
// command: `and make install` runs if the line before succeeded.
func chainsPrevious(line string) bool {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "&&") || strings.HasPrefix(line, "||") {
		return true
	}
	fields := strings.Fields(line)
	return len(fields) > 0 && (fields[0] == "and" || fields[0] == "or")
}

type fishState struct {
	depth   int  // open blocks (begin, if, for, while, function, switch)
	parens  int  // open command substitutions
	quote   byte // ' or " while inside a quoted string
	escaped bool // the previous line ended with a backslash
	pipe    bool // the previous line ended with |, && or ||
}

func (s *fishState) complete() bool {
	return s.depth <= 0 && s.parens <= 0 && s.quote == 0 && !s.escaped && !s.pipe
}

// fishBlocks open a block that a later `end` closes.
var fishBlocks = map[string]bool{"begin": true, "if": true, "for": true, "while": true, "function": true, "switch": true}

// fishPrefixes are followed by another command, not by arguments.
var fishPrefixes = map[string]bool{"begin": true, "if": true, "while": true, "and": true, "or": true, "not": true, "time": true, "else": true, "command": true, "builtin": true, "exec": true}

func (s *fishState) scanLine(line string) {
	// A command starts at the beginning of a line unless a quote or a
	// trailing backslash carries the previous word over.
	cmdPos := s.quote == 0 && !s.escaped
	afterElse := false
	s.escaped, s.pipe = false, false

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch s.quote {
		case '\'':
			if c == '\\' && i+1 < len(line) {
				i++
			} else if c == '\'' {
				s.quote = 0
			}
			continue
		case '"':
			if c == '\\' && i+1 < len(line) {
				i++
			} else if c == '"' {
				s.quote = 0
			}
			continue
		}

		switch c {
		case ' ', '\t':
			continue
		case '\\':
			if i+1 == len(line) {
				s.escaped = true
				return
			}
			i++
			cmdPos, s.pipe = false, false
		case '#':
			return
		case '\'', '"':
			s.quote = c
			cmdPos, s.pipe = false, false
		case ';':
			cmdPos, s.pipe, afterElse = true, false, false
		case '|':
			cmdPos, s.pipe = true, true
		case '&':
			// `&&` chains like a pipe; a lone `&` backgrounds the command.
			cmdPos, s.pipe = true, i+1 < len(line) && line[i+1] == '&'
			if s.pipe {
				i++
			}
		case '(':
			s.parens++
			cmdPos, s.pipe = true, false
		case ')':
			s.parens--
			cmdPos, s.pipe = false, false
		case '<', '>':
			cmdPos, s.pipe = false, false
		default:
			j := i
			for j < len(line) && !strings.ContainsRune(" \t;|&()'\"\\<>", rune(line[j])) {
				j++
			}
			word := line[i:j]
			i = j - 1
			s.pipe = false
			if !cmdPos {
				continue
			}
			switch {
			case word == "end":
				s.depth--
			case fishBlocks[word] && !(word == "if" && afterElse):
				s.depth++
			}
			afterElse = word == "else"
			cmdPos = fishPrefixes[word]
		}
	}
}
