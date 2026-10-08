package ollama

import "testing"

func TestParseHost(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "http://127.0.0.1:11434"},
		{"  ", "http://127.0.0.1:11434"},
		{"localhost", "http://localhost:11434"},
		{"gpu-box:9000", "http://gpu-box:9000"},
		{":9000", "http://127.0.0.1:9000"},
		{"0.0.0.0", "http://0.0.0.0:11434"},
		{"10.0.0.5:11434", "http://10.0.0.5:11434"},
		{"[::1]:8080", "http://[::1]:8080"},
		{"::1", "http://[::1]:11434"},
		{"http://gpu-box", "http://gpu-box:80"},
		{"https://ollama.example.com", "https://ollama.example.com:443"},
		{"https://example.com:8443/ollama", "https://example.com:8443/ollama"},
		{`"gpu-box:9000"`, "http://gpu-box:9000"},
		{"gpu-box:notaport", "http://gpu-box:11434"},
	}
	for _, tt := range tests {
		if got := parseHost(tt.in).String(); got != tt.want {
			t.Errorf("parseHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBaseURLFromEnv(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "gpu-box:9000")
	if got := BaseURL(); got != "http://gpu-box:9000" {
		t.Errorf("BaseURL() = %q, want http://gpu-box:9000", got)
	}
}
