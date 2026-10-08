package embeddings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/meidori/mem/internal/ollama"
)

// Scheme identifies how text is prepared before embedding. Bump it when that
// changes so existing vectors are flagged for reindexing.
const Scheme = "task-prefix-v1"

// taskPrefixes are the instructions some models expect in front of the text,
// telling them whether it is a stored document or a search query. Vectors
// made without them still work, but retrieval is noticeably worse.
var taskPrefixes = map[string]struct{ document, query string }{
	"nomic-embed-text":  {"search_document: ", "search_query: "},
	"mxbai-embed-large": {"", "Represent this sentence for searching relevant passages: "},
}

// baseModel strips the tag and namespace: "library/nomic-embed-text:v1.5"
// becomes "nomic-embed-text".
func baseModel(model string) string {
	model, _, _ = strings.Cut(model, ":")
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}

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

// EmbedDocument embeds text that is stored and later searched.
func (c *Client) EmbedDocument(text string) ([]float32, error) {
	return c.Embed(taskPrefixes[baseModel(c.model)].document + text)
}

// EmbedQuery embeds a search query.
func (c *Client) EmbedQuery(text string) ([]float32, error) {
	return c.Embed(taskPrefixes[baseModel(c.model)].query + text)
}

// Embed embeds text as is, without any task prefix.
func (c *Client) Embed(text string) ([]float32, error) {
	body, err := json.Marshal(map[string]string{
		"model":  c.model,
		"prompt": text,
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Post(c.baseURL+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result.Embedding, nil
}
