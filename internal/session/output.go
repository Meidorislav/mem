package session

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Limits for the output stored with one command.
const (
	maxHeadLines = 20
	maxTailLines = 20
	maxLineRunes = 500
	maxOutput    = 8 << 10
)

var (
	// CSI sequences (colors, cursor movement, erase), OSC strings (titles,
	// hyperlinks) and the two-byte escapes (charset selection, keypad mode).
	ansiCSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	ansiEsc = regexp.MustCompile(`\x1b[()][A-Za-z0-9]|\x1b[=>78cDEHMNOZ]`)
)

// CleanOutput turns raw terminal output into plain text: escape sequences
// are dropped, "\r\n" becomes "\n", a line rewritten with "\r" (progress
// bars) keeps only its final state, and backspaces erase.
func CleanOutput(raw string) string {
	s := strings.ToValidUTF8(raw, "")
	s = ansiOSC.ReplaceAllString(s, "")
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiEsc.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = cleanLine(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func cleanLine(line string) string {
	if strings.Contains(line, "\r") {
		parts := strings.Split(line, "\r")
		line = ""
		for i := len(parts) - 1; i >= 0; i-- {
			if strings.TrimSpace(parts[i]) != "" {
				line = parts[i]
				break
			}
		}
	}

	out := make([]rune, 0, len(line))
	for _, r := range line {
		switch {
		case r == '\b' || r == 0x7f:
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		case r == '\t' || r >= ' ':
			out = append(out, r)
		}
	}
	return strings.TrimRight(string(out), " \t")
}

// TrimOutput shortens cleaned output to what is worth keeping: the first and
// last lines, with a marker for the skipped middle, overlong lines cut, and a
// hard cap on the total size.
func TrimOutput(out string) string {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if utf8.RuneCountInString(l) > maxLineRunes {
			lines[i] = string([]rune(l)[:maxLineRunes]) + "…"
		}
	}
	if skipped := len(lines) - maxHeadLines - maxTailLines; skipped > 0 {
		kept := append([]string{}, lines[:maxHeadLines]...)
		kept = append(kept, fmt.Sprintf("… %d lines skipped …", skipped))
		lines = append(kept, lines[len(lines)-maxTailLines:]...)
	}

	out = strings.Join(lines, "\n")
	if len(out) > maxOutput {
		// Keep the end: that is where the error usually is.
		out = "…" + strings.ToValidUTF8(out[len(out)-maxOutput:], "")
	}
	return out
}

// secretPatterns match credentials that commonly show up in command output.
// Each match is replaced with redacted; the first group, when there is one,
// is kept (the name of a key whose value is hidden).
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`),
	regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`),
	regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{12,}`),
	// scheme://user:password@host
	regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^\s/:@]+):[^\s/@]+@`),
	// name=value or name: value where the name says it is secret.
	regexp.MustCompile(`(?i)(\b[\w.-]*(?:password|passwd|passphrase|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential|authorization)[\w.-]*["']?\s*[=:]\s*)(?:(?:bearer|basic)\s+)?(?:"[^"\n]*"|'[^'\n]*'|[^\s"',;]{6,})`),
}

const redacted = "[redacted]"

// Redact hides credentials in text so they never reach the disk.
func Redact(s string) string {
	for i, re := range secretPatterns {
		switch {
		case i == 6: // bearer/basic: keep the scheme word
			s = re.ReplaceAllString(s, "$1 "+redacted)
		case i == 7: // keep scheme://user, drop the password
			s = re.ReplaceAllString(s, "$1:"+redacted+"@")
		case i == len(secretPatterns)-1:
			s = re.ReplaceAllString(s, "${1}"+redacted)
		default:
			s = re.ReplaceAllString(s, redacted)
		}
	}
	return s
}

// ProcessOutput is the pipeline applied to captured terminal output before
// it is written to a session file: clean, hide secrets, shorten.
func ProcessOutput(raw string) string {
	return TrimOutput(Redact(CleanOutput(raw)))
}
