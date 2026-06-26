package embeddings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultBaseURL = "http://localhost:11434"

type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewClient(model string) *Client {
	return NewClientWithURL(model, defaultBaseURL)
}

func NewClientWithURL(model, baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{},
	}
}

func (c *Client) Embed(text string) ([]float64, error) {
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
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result.Embedding, nil
}
