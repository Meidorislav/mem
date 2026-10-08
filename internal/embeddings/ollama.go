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

// modelInfo is what mem knows about a specific embedding model.
type modelInfo struct {
	// document and query are the instructions the model expects in front of
	// the text. Vectors made without them still work, but retrieval is
	// noticeably worse.
	document, query string
	// minScore is the cosine similarity that separates related from
	// unrelated memories for this model; 0 means unknown.
	minScore float64
}

var models = map[string]modelInfo{
	// Multilingual; no prefixes. minScore from scripts/calibrate.sh: the
	// right answer ranked first for all 16 paraphrased questions, scoring
	// 0.45-0.72 (0.47 at worst without a keyword match); unrelated questions
	// scored 0.44 at best.
	"bge-m3":            {minScore: 0.45},
	"nomic-embed-text":  {document: "search_document: ", query: "search_query: "},
	"mxbai-embed-large": {query: "Represent this sentence for searching relevant passages: "},
}

// fallbackMinScore is used for models without a measured threshold.
const fallbackMinScore = 0.4

// MinScore returns the default relevance threshold for model's scores.
func MinScore(model string) float64 {
	if s := models[baseModel(model)].minScore; s > 0 {
		return s
	}
	return fallbackMinScore
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
	return c.Embed(models[baseModel(c.model)].document + text)
}

// EmbedQuery embeds a search query.
func (c *Client) EmbedQuery(text string) ([]float32, error) {
	return c.Embed(models[baseModel(c.model)].query + text)
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
		return nil, ollama.RequestError(c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ollama.StatusError(resp, c.model)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result.Embedding, nil
}
