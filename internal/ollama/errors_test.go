package ollama

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestErrorUnreachable(t *testing.T) {
	// Grab a free port, then close the listener so nothing answers on it.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := http.Post(url+"/api/embeddings", "application/json", strings.NewReader("{}"))
	if err == nil {
		t.Fatal("expected a connection error")
	}
	got := RequestError(url, err)
	if !errors.Is(got, ErrUnreachable) {
		t.Errorf("RequestError = %v, want ErrUnreachable", got)
	}
	if !strings.Contains(got.Error(), url) || !strings.Contains(got.Error(), "ollama serve") {
		t.Errorf("message %q lacks the address or the hint", got)
	}
}

func TestStatusError(t *testing.T) {
	tests := []struct {
		code int
		body string
		want string
	}{
		{404, `{"error":"model \"bge-m3\" not found, try pulling it first"}`, "run `ollama pull bge-m3`"},
		{500, `{"error":"out of memory"}`, "ollama returned 500: out of memory"},
		{502, `not json`, "ollama returned 502"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		rec.WriteHeader(tt.code)
		rec.WriteString(tt.body)
		got := StatusError(rec.Result(), "bge-m3")
		if !strings.Contains(got.Error(), tt.want) {
			t.Errorf("StatusError(%d, %s) = %q, want it to contain %q", tt.code, tt.body, got, tt.want)
		}
	}
}
