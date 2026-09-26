package llm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meidori/mem/internal/llm"
	"github.com/meidori/mem/internal/storage"
)

func TestGenerateStreams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "test-llm" {
			t.Errorf("unexpected model: %v", req["model"])
		}
		fmt.Fprintln(w, `{"response":"Run ","done":false}`)
		fmt.Fprintln(w, `{"response":"ls -lhS","done":false}`)
		fmt.Fprintln(w, `{"response":"","done":true}`)
	}))
	defer srv.Close()

	var out strings.Builder
	if err := llm.NewClientWithURL("test-llm", srv.URL).Generate("q", &out); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.String() != "Run ls -lhS" {
		t.Errorf("output = %q, want %q", out.String(), "Run ls -lhS")
	}
}

func TestGenerateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if err := llm.NewClientWithURL("missing", srv.URL).Generate("q", &strings.Builder{}); err == nil {
		t.Error("expected error for non-200 response")
	}
}

func TestBuildPrompt(t *testing.T) {
	prompt := llm.BuildPrompt("how to list large files?", []storage.Memory{{
		Title:    "list files by size",
		Tags:     []string{"files"},
		Commands: []storage.Command{{Command: "ls -lhS"}},
	}})

	for _, want := range []string{"Memory 1: list files by size", "Tags: files", "$ ls -lhS", "Question: how to list large files?"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
