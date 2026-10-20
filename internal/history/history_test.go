package history

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseZshExtended(t *testing.T) {
	// Bytes as written by zsh 5.9 with extended_history: "у" (D1 83) is
	// metafied to D1 83 A3, and multi-line commands end lines with "\".
	data := []byte(": 1791906874:0;echo привет \xd1\x83\xa3\xd1\x83\xa4\xd1\x83\xa5 мир\n" +
		": 1791906875:0;for i in 1 2; do\\\n  echo $i\\\ndone\n" +
		": 1791906876:0;ls -lhS\n" +
		": 1791906877:3;printf 'a\\\\'\n")

	want := []string{
		"echo привет уфх мир",
		"for i in 1 2; do\n  echo $i\ndone",
		"ls -lhS",
		`printf 'a\\'`,
	}
	if got := Parse(data); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse =\n%q\nwant\n%q", got, want)
	}
}

func TestParsePlainAndBash(t *testing.T) {
	data := []byte("#1696000000\ngit log --oneline\n#1696000001\ndocker ps -a\n\nkubectl get pods\n: not a header\n")
	want := []string{"git log --oneline", "docker ps -a", "kubectl get pods", ": not a header"}
	if got := Parse(data); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %q, want %q", got, want)
	}
}

func TestParseFish(t *testing.T) {
	// As written by fish 3.7: `echo 'a\\b'` stores each backslash doubled,
	// newlines become \n, and entries may carry a paths list.
	data := []byte("- cmd: echo 'a\\\\\\\\b' привет\n  when: 1792488812\n" +
		"- cmd: for i in 1 2\\n  echo $i\\nend\n  when: 1792488813\n" +
		"- cmd: cat notes.txt\n  when: 1792488814\n  paths:\n    - notes.txt\n")
	want := []string{
		`echo 'a\\b' привет`,
		"for i in 1 2\n  echo $i\nend",
		"cat notes.txt",
	}
	if got := Parse(data); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse =\n%q\nwant\n%q", got, want)
	}
}

func TestDefaultFileFish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")
	t.Setenv("XDG_DATA_HOME", "")
	if got, _ := DefaultFile(); got != filepath.Join(home, ".local/share/fish/fish_history") {
		t.Errorf("DefaultFile = %q", got)
	}
	t.Setenv("XDG_DATA_HOME", "/data")
	if got, _ := DefaultFile(); got != "/data/fish/fish_history" {
		t.Errorf("DefaultFile with XDG_DATA_HOME = %q", got)
	}
}

func TestInteresting(t *testing.T) {
	for cmd, want := range map[string]bool{
		"ls":                      false,
		"ls -lhS":                 true,
		"cd ~/projects/mem":       false,
		"git status":              false,
		"git log --oneline":       true,
		"mem ask nginx":           false,
		"/usr/bin/mem list":       false,
		"vim":                     false,
		"vim /etc/hosts":          true,
		"du":                      false,
		"kubectl get pods":        true,
		"docker system prune -af": true,
	} {
		if got := Interesting(cmd); got != want {
			t.Errorf("Interesting(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestRecent(t *testing.T) {
	cmds := []string{"docker ps", "ls", "kubectl get pods", "docker ps", "cd /tmp", "make test", "git status"}
	want := []string{"kubectl get pods", "docker ps", "make test"}
	if got := Recent(cmds, 10); !reflect.DeepEqual(got, want) {
		t.Errorf("Recent = %q, want %q", got, want)
	}
	if got := Recent(cmds, 2); !reflect.DeepEqual(got, []string{"docker ps", "make test"}) {
		t.Errorf("Recent(2) = %q", got)
	}
}
