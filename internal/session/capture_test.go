package session

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func newSessionFile(t *testing.T) string {
	t.Helper()
	path, err := Create(t.TempDir(), "bash")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecorderCapturesBetweenMarkers(t *testing.T) {
	path := newSessionFile(t)
	if err := os.WriteFile(path, []byte("ls\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	var shown bytes.Buffer
	r := NewRecorder(&shown, path)
	// Markers are split across writes on purpose.
	for _, chunk := range []string{
		"prompt$ ", "\x1b]777;mem", ";start\a", "\x1b[32mfile.txt\x1b[0m\r\n", "\x1b]777;mem;en", "d\a", "prompt$ ",
	} {
		if n, err := r.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}

	if want := "prompt$ \x1b[32mfile.txt\x1b[0m\r\nprompt$ "; shown.String() != want {
		t.Errorf("forwarded %q, want %q", shown.String(), want)
	}
	entries, err := ReadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Entry{{"ls", "file.txt"}}; !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %q, want %q", entries, want)
	}
}

func TestRecorderSkipAndAltScreen(t *testing.T) {
	path := newSessionFile(t)
	r := NewRecorder(io.Discard, path)
	for _, chunk := range []string{
		"\x1b]777;mem;start\a", "hidden", "\x1b]777;mem;skip\a",
		"\x1b]777;mem;start\a", "\x1b[?1049hfull screen", "\x1b]777;mem;end\a",
		"\x1b]777;mem;end\a",                         // stray end without a start
		"\x1b]777;mem;start\a", "\x1b]777;mem;end\a", // no output
	} {
		r.Write([]byte(chunk))
	}
	if data, _ := os.ReadFile(path); len(data) != 0 {
		t.Errorf("session file = %q, want empty", data)
	}
}

func TestRecorderOrphanOutputDropped(t *testing.T) {
	path := newSessionFile(t)
	// `mem remember` truncated the file while its own command was running.
	r := NewRecorder(io.Discard, path)
	r.Write([]byte("\x1b]777;mem;start\aRemembered #1\r\n\x1b]777;mem;end\a"))
	os.WriteFile(path, append(mustRead(t, path), []byte("echo hi\x00")...), 0o600)

	entries, _ := ReadEntries(path)
	if want := []Entry{{Command: "echo hi"}}; !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %q, want %q", entries, want)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRecorderLongOutputKeepsEnds(t *testing.T) {
	path := newSessionFile(t)
	r := NewRecorder(io.Discard, path)
	r.Write([]byte("\x1b]777;mem;start\a"))
	for i := 0; i < 5000; i++ {
		r.Write([]byte(strings.Repeat("filler ", 10) + "\r\n"))
	}
	r.Write([]byte("last line\r\n\x1b]777;mem;end\a"))

	os.WriteFile(path, append([]byte("cmd\x00"), mustRead(t, path)...), 0o600)
	entries, _ := ReadEntries(path)
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Output, "last line") || len(entries[0].Output) > maxOutput+4 {
		t.Fatalf("entries = %.80q", entries)
	}
}

func TestRunCapturedBash(t *testing.T) {
	bash, _, err := ResolveShell("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	// Output capture needs PS0, added in bash 4.4 (macOS ships 3.2).
	ver, err := exec.Command(bash, "-c", `echo "${BASH_VERSINFO[0]}.${BASH_VERSINFO[1]}"`).Output()
	if err != nil {
		t.Skipf("bash version: %v", err)
	}
	var major, minor int
	fmt.Sscanf(string(ver), "%d.%d", &major, &minor)
	if major < 4 || (major == 4 && minor < 4) {
		t.Skipf("bash %d.%d has no PS0", major, minor)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "")
	os.WriteFile(filepath.Join(home, ".bashrc"), nil, 0o600)

	path := newSessionFile(t)
	sh, err := NewShell("bash", path)
	if err != nil {
		t.Skip(err)
	}
	defer sh.Cleanup()

	// A pty pair stands in for the user's terminal.
	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer master.Close()
	defer tty.Close()

	var shown bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- sh.RunCaptured(tty, &shown, path) }()

	script := "echo hello\nprintf 'a\\nb\\n'\nls /nonexistent-dir\n true | cat\n exit\n"
	go func() {
		for _, line := range strings.SplitAfter(script, "\n") {
			time.Sleep(300 * time.Millisecond)
			master.WriteString(line)
		}
	}()
	go io.Copy(io.Discard, master)

	select {
	case err := <-done:
		if err != nil {
			t.Logf("shell exited: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("shell did not exit")
	}

	entries, err := ReadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	got := CleanEntries(entries, "bash")
	t.Logf("entries: %q", got)
	if len(got) < 3 {
		t.Fatalf("got %d entries, want at least 3", len(got))
	}
	want := []Entry{
		{"echo hello", "hello"},
		{"printf 'a\\nb\\n'", "a\nb"},
	}
	if !reflect.DeepEqual(got[:2], want) {
		t.Errorf("entries = %q, want prefix %q", got, want)
	}
	if got[2].Command != "ls /nonexistent-dir" || !strings.Contains(got[2].Output, "nonexistent-dir") {
		t.Errorf("third entry = %q", got[2])
	}
	if strings.Contains(shown.String(), "777;mem") {
		t.Errorf("markers leaked to the terminal: %q", shown.String())
	}
}
