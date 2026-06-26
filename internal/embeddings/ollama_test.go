package embeddings_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meidori/mem/internal/embeddings"
)

func TestEmbed(t *testing.T) {
	want := []float64{0.1, 0.2, 0.3}

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
