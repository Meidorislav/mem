// Package llm talks to a local Ollama model to synthesize answers from
// retrieved memories.
package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/meidori/mem/internal/ollama"
	"github.com/meidori/mem/internal/storage"
)

type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewClient(model string) *Client {
	return NewClientWithURL(model, ollama.BaseURL())
}

func NewClientWithURL(model, baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{},
	}
}

// Generate streams the model's completion of prompt into w.
func (c *Client) Generate(prompt string, w io.Writer) error {
	body, err := json.Marshal(map[string]any{
		"model":  c.model,
		"prompt": prompt,
		"stream": true,
	})
	if err != nil {
		return err
	}

	resp, err := c.http.Post(c.baseURL+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var chunk struct {
			Response string `json:"response"`
			Done     bool   `json:"done"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
		if chunk.Error != "" {
			return fmt.Errorf("ollama: %s", chunk.Error)
		}
		if _, err := io.WriteString(w, chunk.Response); err != nil {
			return err
		}
		if chunk.Done {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	return nil
}

// BuildPrompt asks the model to answer question using only the given memories.
func BuildPrompt(question string, memories []storage.Memory) string {
	var sb strings.Builder
	sb.WriteString("You are a terse assistant answering questions about the user's own saved terminal history and notes.\n")
	sb.WriteString("Answer using ONLY the memories below. Quote the exact commands that apply, in order.\n")
	sb.WriteString("If the memories do not answer the question, say so in one sentence.\n\n")

	for i, m := range memories {
		fmt.Fprintf(&sb, "Memory %d: %s\n", i+1, m.Title)
		if len(m.Tags) > 0 {
			fmt.Fprintf(&sb, "Tags: %s\n", strings.Join(m.Tags, ", "))
		}
		if m.Description != "" {
			fmt.Fprintf(&sb, "Description: %s\n", m.Description)
		}
		for _, c := range m.Commands {
			fmt.Fprintf(&sb, "$ %s\n", c.Command)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "Question: %s\nAnswer:", question)
	return sb.String()
}
