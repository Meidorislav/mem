package session

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCreateAndLatest(t *testing.T) {
	dir := t.TempDir()

	if _, err := Latest(dir); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Latest on empty dir: err = %v, want ErrNoSession", err)
	}

	first, err := Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(time.Millisecond)
	second, err := Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first == second {
		t.Fatalf("Create returned the same path twice: %s", first)
	}

	latest, err := Latest(dir)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest != second {
		t.Errorf("Latest = %s, want %s", latest, second)
	}
}

func TestRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s"+fileExt)
	data := "ssh prod\x00for i in 1 2; do\n  echo $i\ndone\x00\x00journalctl -u api\x00"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []string{"ssh prod", "for i in 1 2; do\n  echo $i\ndone", "journalctl -u api"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %q, want %q", got, want)
	}
}

func TestReadMissing(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "nope")); !errors.Is(err, ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

func TestClean(t *testing.T) {
	in := []string{
		"  ",
		"ssh prod",
		"ssh prod",
		"mem watch",
		"/usr/local/bin/mem remember fix",
		"journalctl -u api -n 100 ",
		"exit",
		"systemctl restart api",
		"ssh prod",
	}
	want := []string{"ssh prod", "journalctl -u api -n 100", "systemctl restart api", "ssh prod"}
	if got := Clean(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Clean = %q, want %q", got, want)
	}
}

func TestNewShellUnsupported(t *testing.T) {
	if _, err := NewShell("sh", "/tmp/x"); err == nil {
		t.Error("NewShell(sh) succeeded, want unsupported shell error")
	}
}

func TestNewShellBash(t *testing.T) {
	sh, err := NewShell("bash", "/tmp/x.session")
	if err != nil {
		t.Skipf("bash not available: %v", err)
	}
	defer sh.Cleanup()

	if sh.Name != "bash" {
		t.Errorf("Name = %q, want bash", sh.Name)
	}
	found := false
	for _, kv := range sh.Cmd().Env {
		if kv == EnvVar+"=/tmp/x.session" {
			found = true
		}
	}
	if !found {
		t.Errorf("shell env lacks %s", EnvVar)
	}
}
