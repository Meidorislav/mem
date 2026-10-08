// Package paths locates mem's data directory.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvVar overrides the data directory, e.g. to keep a scratch database for
// experiments apart from the real one.
const EnvVar = "MEM_HOME"

// AppDir returns where mem keeps its data: $MEM_HOME, or ~/.mem.
func AppDir() (string, error) {
	if dir := os.Getenv(EnvVar); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	return filepath.Join(home, ".mem"), nil
}
