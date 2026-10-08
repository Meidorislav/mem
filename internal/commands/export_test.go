package commands

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/meidori/mem/internal/storage"
)

func sampleMemories() []storage.Memory {
	out := "listening on 8080"
	return []storage.Memory{
		{
			ID: 1, Title: "restart api", Description: "api crash on prod", Source: storage.SourceRemember,
			Tags:      []string{"ops", "prod"},
			Commands:  []storage.Command{{Command: "journalctl -u api"}, {Command: "for i in 1 2; do\n  echo $i\ndone", Output: &out}},
			CreatedAt: time.Date(2024, 3, 15, 9, 30, 0, 0, time.UTC),
			UpdatedAt: time.Date(2024, 4, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID: 2, Title: "markdown fence", Source: storage.SourceSave,
			Commands:  []storage.Command{{Command: "echo '```'"}},
			CreatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		},
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, sampleMemories(), time.Now()); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	got, err := readExport(&buf)
	if err != nil {
		t.Fatalf("readExport: %v", err)
	}
	want := sampleMemories()
	if len(got) != len(want) {
		t.Fatalf("got %d memories, want %d", len(got), len(want))
	}
	for i, g := range got {
		w := want[i]
		if g.ID != 0 {
			t.Errorf("memory %d: ID = %d, want 0 (IDs are not exported)", i, g.ID)
		}
		if g.Title != w.Title || g.Description != w.Description || g.Source != w.Source ||
			strings.Join(g.Tags, ",") != strings.Join(w.Tags, ",") ||
			!g.CreatedAt.Equal(w.CreatedAt) || !g.UpdatedAt.Equal(w.UpdatedAt) {
			t.Errorf("memory %d = %+v, want %+v", i, g, w)
		}
		if len(g.Commands) != len(w.Commands) {
			t.Fatalf("memory %d: %d commands, want %d", i, len(g.Commands), len(w.Commands))
		}
		for j := range g.Commands {
			if g.Commands[j].Command != w.Commands[j].Command {
				t.Errorf("memory %d command %d = %q, want %q", i, j, g.Commands[j].Command, w.Commands[j].Command)
			}
		}
	}
	if got[0].Commands[1].Output == nil || *got[0].Commands[1].Output != "listening on 8080" {
		t.Errorf("command output not round-tripped: %+v", got[0].Commands[1])
	}
}

func TestReadExportValidation(t *testing.T) {
	for name, input := range map[string]string{
		"not json":       "hello",
		"future version": `{"version": 99, "memories": []}`,
		"no version":     `{"memories": []}`,
		"empty title":    `{"version": 1, "memories": [{"title": "  "}]}`,
	} {
		if _, err := readExport(strings.NewReader(input)); err == nil {
			t.Errorf("%s: readExport succeeded, want error", name)
		}
	}

	got, err := readExport(strings.NewReader(`{"version": 1, "memories": [{"title": "x", "source": "bogus", "commands": [{"command": " "}, {"command": "ls"}]}]}`))
	if err != nil {
		t.Fatalf("readExport: %v", err)
	}
	if got[0].Source != storage.SourceSave || len(got[0].Commands) != 1 {
		t.Errorf("got %+v, want unknown source mapped to save and blank commands dropped", got[0])
	}
}

func TestWriteMarkdown(t *testing.T) {
	var buf bytes.Buffer
	if err := writeMarkdown(&buf, sampleMemories(), time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("writeMarkdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"2 memories, exported 2026-10-08",
		"## restart api",
		"_#1 · 2024-03-15 · remember · tags: ops, prod_",
		"api crash on prod",
		"```sh\njournalctl -u api\nfor i in 1 2; do\n  echo $i\ndone\n```",
		"````sh\necho '```'\n````", // fence longer than the backticks inside
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}
