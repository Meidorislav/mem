package commands

import (
	"testing"

	"github.com/meidori/mem/internal/storage"
)

func TestChunkMemory(t *testing.T) {
	cmdID := int64(10)
	m := &storage.Memory{
		ID:          1,
		Title:       "Test memory",
		Description: "A test description",
		Tags:        []string{"test", "go"},
		Commands: []storage.Command{
			{ID: cmdID, Command: "ls -la"},
		},
	}

	chunks := ChunkMemory(m)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// First chunk should be context
	if chunks[0].CommandID != nil {
		t.Errorf("expected context chunk to have nil CommandID, got %v", *chunks[0].CommandID)
	}
	expectedContext := "context | note: Test memory | desc: A test description | tags: test, go"
	if chunks[0].Text != expectedContext {
		t.Errorf("expected context %q, got %q", expectedContext, chunks[0].Text)
	}

	// Second chunk should be the command
	if chunks[1].CommandID == nil || *chunks[1].CommandID != cmdID {
		t.Errorf("expected command chunk to have CommandID %d", cmdID)
	}
	expectedCmd := "ls -la | note: Test memory | desc: A test description | tags: test, go"
	if chunks[1].Text != expectedCmd {
		t.Errorf("expected command %q, got %q", expectedCmd, chunks[1].Text)
	}
}

func TestChunkMemoryNoCommands(t *testing.T) {
	m := &storage.Memory{
		ID:    2,
		Title: "Empty note",
	}

	chunks := ChunkMemory(m)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	if chunks[0].CommandID != nil {
		t.Errorf("expected context chunk to have nil CommandID")
	}
	expectedContext := "context | note: Empty note"
	if chunks[0].Text != expectedContext {
		t.Errorf("expected context %q, got %q", expectedContext, chunks[0].Text)
	}
}
