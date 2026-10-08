package commands

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meidori/mem/internal/storage"
)

func TestEditRoundTrip(t *testing.T) {
	m := &storage.Memory{
		ID:          7,
		Title:       "restart api",
		Description: "api-service crash on prod",
		Tags:        []string{"ops", "prod"},
		Commands: []storage.Command{
			{Command: "journalctl -u api -n 100"},
			{Command: "for i in 1 2; do\n  echo $i\ndone"},
			{Command: "cat <<EOF > conf\n# not a comment\nEOF"},
		},
	}

	got, err := parseEdit(formatForEdit(m))
	if err != nil {
		t.Fatalf("parseEdit: %v", err)
	}
	if got.Title != m.Title || got.Description != m.Description || !reflect.DeepEqual(got.Tags, m.Tags) {
		t.Errorf("fields = %q / %q / %v, want %q / %q / %v", got.Title, got.Description, got.Tags, m.Title, m.Description, m.Tags)
	}
	if len(got.Commands) != len(m.Commands) {
		t.Fatalf("got %d commands, want %d: %+v", len(got.Commands), len(m.Commands), got.Commands)
	}
	for i := range m.Commands {
		if got.Commands[i].Command != m.Commands[i].Command {
			t.Errorf("command %d = %q, want %q", i, got.Commands[i].Command, m.Commands[i].Command)
		}
	}
}

func TestParseEdit(t *testing.T) {
	text := `# comment
title:   new title
tags: a,  b , ,c
description:

$ ls -lhS

$ du -sh *
$
`
	got, err := parseEdit(text)
	if err != nil {
		t.Fatalf("parseEdit: %v", err)
	}
	if got.Title != "new title" || got.Description != "" || !reflect.DeepEqual(got.Tags, []string{"a", "b", "c"}) {
		t.Errorf("fields = %+v", got)
	}
	var cmds []string
	for _, c := range got.Commands {
		cmds = append(cmds, c.Command)
	}
	if want := []string{"ls -lhS", "du -sh *"}; !reflect.DeepEqual(cmds, want) {
		t.Errorf("commands = %q, want %q", cmds, want)
	}
}

func TestParseEditErrors(t *testing.T) {
	for _, text := range []string{
		"title: x\nauthor: me\n",
		"just some text\n",
	} {
		if _, err := parseEdit(text); err == nil {
			t.Errorf("parseEdit(%q) succeeded, want error", text)
		}
	}
}

func TestFormatForEditShowsHelp(t *testing.T) {
	out := formatForEdit(&storage.Memory{ID: 3, Title: "t"})
	if !strings.Contains(out, "# Edit memory #3") || !strings.Contains(out, "title: t\n") {
		t.Errorf("unexpected template:\n%s", out)
	}
}
