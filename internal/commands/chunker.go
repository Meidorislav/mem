package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/meidori/mem/internal/storage"
)

type Chunk struct {
	Text       string
	Hash       string
	CommandID  *int64
	ChunkIndex int
}

func ChunkMemory(m *storage.Memory) []Chunk {
	var chunks []Chunk
	index := 0

	tagsStr := ""
	if len(m.Tags) > 0 {
		tagsStr = fmt.Sprintf(" | tags: %s", strings.Join(m.Tags, ", "))
	}
	noteStr := ""
	if m.Title != "" {
		noteStr = fmt.Sprintf(" | note: %s", m.Title)
	}
	descStr := ""
	if m.Description != "" {
		descStr = fmt.Sprintf(" | desc: %s", m.Description)
	}

	// 1. Context chunk (if no commands, or if description exists)
	if len(m.Commands) == 0 || m.Description != "" {
		text := fmt.Sprintf("context%s%s%s", noteStr, descStr, tagsStr)
		chunks = append(chunks, Chunk{
			Text:       text,
			Hash:       hashText(text),
			CommandID:  nil,
			ChunkIndex: index,
		})
		index++
	}

	// 2. Command chunks
	for _, cmd := range m.Commands {
		text := fmt.Sprintf("%s%s%s%s", cmd.Command, noteStr, descStr, tagsStr)
		
		// Create a local copy of ID since we take pointer
		cmdID := cmd.ID
		
		chunks = append(chunks, Chunk{
			Text:       text,
			Hash:       hashText(text),
			CommandID:  &cmdID,
			ChunkIndex: index,
		})
		index++
	}

	return chunks
}

func hashText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}
