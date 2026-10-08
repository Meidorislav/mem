// Package ollama holds settings shared by the Ollama API clients.
package ollama

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const defaultPort = "11434"

// BaseURL returns the Ollama server address from OLLAMA_HOST, defaulting to
// http://127.0.0.1:11434. It accepts the same forms as the ollama CLI:
// "host", "host:port", ":port", "http://host:port" and "https://host/path".
func BaseURL() string {
	return parseHost(os.Getenv("OLLAMA_HOST")).String()
}

func parseHost(raw string) *url.URL {
	s := strings.Trim(strings.TrimSpace(raw), "\"'")

	scheme, hostport, ok := strings.Cut(s, "://")
	port := defaultPort
	switch {
	case !ok:
		scheme, hostport = "http", s
	case scheme == "http":
		port = "80"
	case scheme == "https":
		port = "443"
	}

	hostport, path, _ := strings.Cut(hostport, "/")

	host, p, err := net.SplitHostPort(hostport)
	if err != nil {
		// No port given: hostport is a bare host, an IP or empty.
		host = "127.0.0.1"
		if ip := net.ParseIP(strings.Trim(hostport, "[]")); ip != nil {
			host = ip.String()
		} else if hostport != "" {
			host = hostport
		}
	} else {
		if host == "" {
			host = "127.0.0.1"
		}
		port = p
	}

	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		port = defaultPort
	}

	return &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(host, port),
		Path:   strings.TrimSuffix("/"+path, "/"),
	}
}
