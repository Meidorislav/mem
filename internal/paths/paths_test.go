package paths

import (
	"path/filepath"
	"testing"
)

func TestAppDir(t *testing.T) {
	t.Setenv(EnvVar, "")
	t.Setenv("HOME", "/home/someone")
	if got, _ := AppDir(); got != filepath.Join("/home/someone", ".mem") {
		t.Errorf("AppDir() = %q, want ~/.mem", got)
	}

	t.Setenv(EnvVar, "/tmp/scratch-mem")
	if got, _ := AppDir(); got != "/tmp/scratch-mem" {
		t.Errorf("AppDir() with %s = %q, want /tmp/scratch-mem", EnvVar, got)
	}
}
