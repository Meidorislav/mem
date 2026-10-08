package commands

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/meidori/mem/internal/storage"
)

func memoryWith(cmds ...string) storage.Memory {
	m := storage.Memory{ID: 4, Title: "restart api"}
	for _, c := range cmds {
		m.Commands = append(m.Commands, storage.Command{Command: c})
	}
	return m
}

func TestChooseCommands(t *testing.T) {
	three := memoryWith("journalctl -u api", "systemctl restart api", "curl localhost:8080/health")

	tests := []struct {
		name        string
		m           storage.Memory
		input       string
		interactive bool
		want        []string
		wantErr     error
	}{
		{"single command needs no prompt", memoryWith("ls -lhS"), "", true, []string{"ls -lhS"}, nil},
		{"pick by number", three, "2\n", true, []string{"systemctl restart api"}, nil},
		{"all", three, "a\n", true, texts(three), nil},
		{"enter cancels", three, "\n", true, nil, errCancelled},
		{"EOF cancels", three, "", true, nil, errCancelled},
		{"not interactive takes all", three, "", false, texts(three), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			got, err := chooseCommands(tt.m, bufio.NewReader(strings.NewReader(tt.input)), &out, tt.interactive)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	for _, bad := range []string{"0\n", "4\n", "x\n"} {
		if _, err := chooseCommands(three, bufio.NewReader(strings.NewReader(bad)), &strings.Builder{}, true); err == nil || errors.Is(err, errCancelled) {
			t.Errorf("input %q: err = %v, want invalid choice", bad, err)
		}
	}
	if _, err := chooseCommands(memoryWith(), nil, nil, true); err == nil {
		t.Error("memory without commands: want error")
	}
}

func TestConfirm(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "YES\n": true, "да\n": true, "\n": false, "n\n": false, "": false} {
		if got := confirm(bufio.NewReader(strings.NewReader(input)), &strings.Builder{}, "Run?"); got != want {
			t.Errorf("confirm(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestRunCommands(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	var out strings.Builder

	err := runCommands([]string{"echo first", "touch " + marker, "exit 3", "echo never"}, "sh", strings.NewReader(""), &out, &out)
	if err == nil || !strings.Contains(err.Error(), "exited with status 3") {
		t.Fatalf("err = %v, want exit status 3", err)
	}
	if !strings.Contains(out.String(), "first") || strings.Contains(out.String(), "never") {
		t.Errorf("output = %q, want commands up to the failure only", out.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("command before the failure did not run: %v", err)
	}
}

func TestCopyToClipboardWithoutTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := copyToClipboard("ls"); err == nil || !strings.Contains(err.Error(), "no clipboard tool") {
		t.Errorf("err = %v, want no clipboard tool", err)
	}
}

func texts(m storage.Memory) []string {
	out := make([]string, len(m.Commands))
	for i, c := range m.Commands {
		out[i] = c.Command
	}
	return out
}
