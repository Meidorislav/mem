package ollama

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// ErrUnreachable means nothing is listening at the Ollama address.
var ErrUnreachable = errors.New("ollama is not running")

// RequestError explains a failed HTTP call to the Ollama server at baseURL.
func RequestError(baseURL string, err error) error {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return fmt.Errorf("%w at %s: start it with `ollama serve` or the Ollama app", ErrUnreachable, baseURL)
	}
	return fmt.Errorf("ollama request: %w", err)
}

// StatusError explains a non-200 response, using the {"error": "..."} body
// Ollama sends. A missing model gets a hint to pull it.
func StatusError(resp *http.Response, model string) error {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)

	switch {
	case resp.StatusCode == http.StatusNotFound && strings.Contains(body.Error, "not found"):
		return fmt.Errorf("model %q is not in Ollama: run `ollama pull %s`", model, model)
	case body.Error != "":
		return fmt.Errorf("ollama returned %d: %s", resp.StatusCode, body.Error)
	default:
		return fmt.Errorf("ollama returned %d", resp.StatusCode)
	}
}
