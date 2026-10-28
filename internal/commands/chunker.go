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
		text := fmt.Sprintf("%s%s%s%s%s", cmd.Command, outputStr(cmd.Output), noteStr, descStr, tagsStr)

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

// Only the end of a command's output goes into its chunk: that is where the
// error or the result is, and the rest would drown the command itself. Full
// output is searchable through the keyword index.
const (
	outputTailLines = 3
	outputTailRunes = 240
)

// outputStr renders the tail of out as " | output: ..." for a chunk, or ""
// when there is none.
func outputStr(out *string) string {
	if out == nil {
		return ""
	}
	var tail []string
	lines := strings.Split(*out, "\n")
	for i := len(lines) - 1; i >= 0 && len(tail) < outputTailLines; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			tail = append([]string{l}, tail...)
		}
	}
	if len(tail) == 0 {
		return ""
	}
	text := strings.Join(tail, " / ")
	if r := []rune(text); len(r) > outputTailRunes {
		text = "…" + string(r[len(r)-outputTailRunes:])
	}
	return " | output: " + text
}

func hashText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}
