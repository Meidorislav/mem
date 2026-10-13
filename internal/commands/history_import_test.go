package commands

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSelection(t *testing.T) {
	tests := []struct {
		in   string
		want [][]int
	}{
		{"3", [][]int{{2}}},
		{"3, 7-9", [][]int{{2}, {6, 7, 8}}},
		{"1 4, 2", [][]int{{0, 3}, {1}}},
		{"5-5 5", [][]int{{4}}}, // duplicates within a memory collapse
		{"  \n", nil},
		{",,", nil},
	}
	for _, tt := range tests {
		got, err := parseSelection(tt.in, 10)
		if err != nil {
			t.Fatalf("parseSelection(%q): %v", tt.in, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseSelection(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
	for _, bad := range []string{"0", "11", "3-1", "x", "2-", "1-11"} {
		if _, err := parseSelection(bad, 10); err == nil {
			t.Errorf("parseSelection(%q) succeeded, want error", bad)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Restart the API service":              "Restart the API service",
		"\"Перезапуск api на проде\"\n":        "Перезапуск api на проде",
		"Title: Find large files":              "Find large files",
		"\n\n- «Почистить docker»\nExtra text": "Почистить docker",
		"   ":                    "",
		strings.Repeat("я", 100): strings.Repeat("я", 80),
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitlePrompt(t *testing.T) {
	p := titlePrompt([]string{"docker system prune -af"}, []string{"перезапуск api на проде"})
	for _, want := range []string{"$ docker system prune -af", "- перезапуск api на проде", "language and style"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(titlePrompt([]string{"ls"}, nil), "language and style") {
		t.Error("prompt mentions example titles when there are none")
	}
}
