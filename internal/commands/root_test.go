package commands

import (
	"strings"
	"testing"

	"github.com/meidori/mem/internal/storage"
)

func TestFormatSearchResults_Empty(t *testing.T) {
	out := formatSearchResults(nil)
	if out != "No matching memories found." {
		t.Errorf("expected 'No matching memories found.', got %q", out)
	}
}

func TestFormatSearchResults_SingleMemory(t *testing.T) {
	memories := []storage.Memory{
		{
			Title:       "list files by size",
			Description: "Shows files ordered by size in human-readable format",
			Tags:        []string{"files", "disk"},
			Commands: []storage.Command{
				{Command: "ls -lhS"},
			},
		},
	}

	out := formatSearchResults(memories)
	if !strings.Contains(out, "1. list files by size  [files, disk]") {
		t.Errorf("expected title and tags in output, got %q", out)
	}
	if !strings.Contains(out, "Shows files ordered by size") {
		t.Errorf("expected description in output, got %q", out)
	}
	if !strings.Contains(out, "$ ls -lhS") {
		t.Errorf("expected command in output, got %q", out)
	}
}

func TestFormatSearchResults_MultipleMemories(t *testing.T) {
	memories := []storage.Memory{
		{
			Title: "first memory",
			Commands: []storage.Command{
				{Command: "echo first"},
			},
		},
		{
			Title: "second memory",
			Commands: []storage.Command{
				{Command: "echo second"},
			},
		},
	}

	out := formatSearchResults(memories)
	if !strings.Contains(out, "1. first memory") {
		t.Errorf("expected memory 1 in output, got %q", out)
	}
	if !strings.Contains(out, "2. second memory") {
		t.Errorf("expected memory 2 in output, got %q", out)
	}
}
