package session

import (
	"fmt"
	"strings"
	"testing"
)

func TestCleanOutput(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"colors", "\x1b[31merror\x1b[0m: boom\r\n", "error: boom"},
		{"crlf", "a\r\nb\r\n", "a\nb"},
		{"progress", "10%\r50%\r100%\r\ndone\r\n", "100%\ndone"},
		{"backspace", "helo\blo", "hello"},
		{"osc title", "\x1b]0;user@host\x07out", "out"},
		{"osc hyperlink", "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\", "link"},
		{"cursor", "\x1b[2K\x1b[1Gready", "ready"},
		{"charset", "\x1b(Bplain", "plain"},
		{"trailing space", "a   \nb\t\n", "a\nb"},
		{"utf8", "ошибка: нет доступа\r\n", "ошибка: нет доступа"},
		{"bad utf8", "a\xffb", "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanOutput(tt.in); got != tt.want {
				t.Errorf("CleanOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTrimOutput(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	got := strings.Split(TrimOutput(strings.Join(lines, "\n")), "\n")
	if len(got) != maxHeadLines+1+maxTailLines {
		t.Fatalf("got %d lines, want %d", len(got), maxHeadLines+1+maxTailLines)
	}
	if got[0] != "line 1" || got[maxHeadLines] != "… 60 lines skipped …" || got[len(got)-1] != "line 100" {
		t.Errorf("unexpected trim: first %q, marker %q, last %q", got[0], got[maxHeadLines], got[len(got)-1])
	}

	short := "a\nb"
	if got := TrimOutput(short); got != short {
		t.Errorf("short output changed: %q", got)
	}

	long := strings.Repeat("x", 2000)
	if got := TrimOutput(long); len([]rune(got)) != maxLineRunes+1 {
		t.Errorf("long line kept %d runes, want %d", len([]rune(got)), maxLineRunes+1)
	}

	// Many long lines: the byte cap keeps the end.
	big := strings.Repeat(strings.Repeat("y", 400)+"\n", 30) + "the end"
	if got := TrimOutput(big); len(got) > maxOutput+4 || !strings.HasSuffix(got, "the end") {
		t.Errorf("byte cap: len %d, suffix %q", len(got), got[max(0, len(got)-10):])
	}
}

func TestRedact(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ghp_abcdefghijklmnopqrstuvwxyz0123456789", redacted},
		{"key AKIAIOSFODNN7EXAMPLE end", "key " + redacted + " end"},
		{"Authorization: Bearer abcdefghijklmnop1234", "Authorization: " + redacted},
		{"curl -H 'Bearer abcdefghijklmnop1234'", "curl -H '" + "Bearer " + redacted + "'"},
		{"API_KEY=hunter2hunter2", "API_KEY=" + redacted},
		{"DB_PASSWORD: \"s3 cret\"", "DB_PASSWORD: " + redacted},
		{"export GITHUB_TOKEN='abc def'", "export GITHUB_TOKEN='" + "abc def'"}, // replaced below
		{"postgres://app:pa55w0rd@db:5432/x", "postgres://app:" + redacted + "@db:5432/x"},
		{"token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.sig", "token " + redacted},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----", redacted},
		{"error: connection refused to 10.0.0.1:5432", "error: connection refused to 10.0.0.1:5432"},
		{"tokens: 5 of 10 used", "tokens: 5 of 10 used"},
	}
	// The quoted-value case keeps the quotes out of the replacement.
	tests[6].want = "export GITHUB_TOKEN=" + redacted
	for _, tt := range tests {
		if got := Redact(tt.in); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
