package embeddings_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meidori/mem/internal/embeddings"
)

func TestEmbed(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "test-model" {
			t.Errorf("unexpected model: %s", req["model"])
		}
		json.NewEncoder(w).Encode(map[string]any{"embedding": want})
	}))
	defer srv.Close()

	client := embeddings.NewClientWithURL("test-model", srv.URL)
	got, err := client.Embed("hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %f, want %f", i, got[i], want[i])
		}
	}
}

func TestEmbedServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := embeddings.NewClientWithURL("test-model", srv.URL)
	_, err := client.Embed("hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTaskPrefixes(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		got = append(got, req["prompt"])
		json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{1}})
	}))
	defer srv.Close()

	tests := []struct {
		model, doc, query string
	}{
		{"nomic-embed-text", "search_document: ls -lhS", "search_query: big files"},
		{"nomic-embed-text:v1.5", "search_document: ls -lhS", "search_query: big files"},
		{"mxbai-embed-large:latest", "ls -lhS", "Represent this sentence for searching relevant passages: big files"},
		{"all-minilm", "ls -lhS", "big files"},
	}
	for _, tt := range tests {
		got = nil
		c := embeddings.NewClientWithURL(tt.model, srv.URL)
		if _, err := c.EmbedDocument("ls -lhS"); err != nil {
			t.Fatalf("EmbedDocument: %v", err)
		}
		if _, err := c.EmbedQuery("big files"); err != nil {
			t.Fatalf("EmbedQuery: %v", err)
		}
		if len(got) != 2 || got[0] != tt.doc || got[1] != tt.query {
			t.Errorf("%s: prompts = %q, want [%q %q]", tt.model, got, tt.doc, tt.query)
		}
	}
}

func TestMinScore(t *testing.T) {
	for model, want := range map[string]float64{
		"bge-m3":           0.45,
		"bge-m3:latest":    0.45,
		"nomic-embed-text": 0.4,
		"unknown-model":    0.4,
	} {
		if got := embeddings.MinScore(model); got != want {
			t.Errorf("MinScore(%q) = %v, want %v", model, got, want)
		}
	}
}
