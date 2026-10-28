package session

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCreateAndLatest(t *testing.T) {
	dir := t.TempDir()

	if _, err := Latest(dir); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Latest on empty dir: err = %v, want ErrNoSession", err)
	}

	first, err := Create(dir, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(time.Millisecond)
	second, err := Create(dir, "fish")
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
	if ShellOf(first) != "" || ShellOf(second) != "fish" {
		t.Errorf("ShellOf = %q, %q; want \"\", fish", ShellOf(first), ShellOf(second))
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
	if got := Clean(in, "zsh"); !reflect.DeepEqual(got, want) {
		t.Errorf("Clean = %q, want %q", got, want)
	}
}

func TestNewShellUnsupported(t *testing.T) {
	if _, err := NewShell("sh", "/tmp/x"); err == nil {
		t.Error("NewShell(sh) succeeded, want unsupported shell error")
	}
}

func TestNewShellFish(t *testing.T) {
	sh, err := NewShell("fish", "/tmp/x.fish.session")
	if err != nil {
		t.Skipf("fish not available: %v", err)
	}
	defer sh.Cleanup()

	args := sh.cmd.Args
	if sh.Name != "fish" || len(args) != 4 || args[2] != "-C" || !strings.Contains(args[3], "fish_preexec") {
		t.Errorf("fish shell = %q %q", sh.Name, args)
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
	for _, kv := range sh.cmd.Env {
		if kv == EnvVar+"=/tmp/x.session" {
			found = true
		}
	}
	if !found {
		t.Errorf("shell env lacks %s", EnvVar)
	}
}

func TestCleanSplitsPastedBlock(t *testing.T) {
	// zsh runs a pasted block as a single line, so preexec sees all of it.
	in := []string{"ls -la\ndu -sh * | sort -h\n./mem remember \"тест\"\nexit"}
	want := []string{"ls -la", "du -sh * | sort -h"}
	if got := Clean(in, "zsh"); !reflect.DeepEqual(got, want) {
		t.Errorf("Clean = %q, want %q", got, want)
	}
}

func TestCleanKeepsMultiLineConstructs(t *testing.T) {
	in := []string{
		"for i in 1 2; do\n  echo $i\ndone",
		"docker run \\\n  --rm alpine",
		"cat <<EOF > conf\na=1\nEOF",
		"cd /tmp; ls\npwd",
		"make build\n# then run it\n./bin/app",
		"if [ -f x ]; then\n  echo 'unterminated\n", // does not parse: kept as is
	}
	want := []string{
		"for i in 1 2; do\n  echo $i\ndone",
		"docker run \\\n  --rm alpine",
		"cat <<EOF > conf\na=1\nEOF",
		"cd /tmp; ls",
		"pwd",
		"make build",
		"./bin/app",
		"if [ -f x ]; then\n  echo 'unterminated",
	}
	if got := Clean(in, "zsh"); !reflect.DeepEqual(got, want) {
		t.Errorf("Clean =\n%q\nwant\n%q", got, want)
	}
}

func TestCleanFish(t *testing.T) {
	in := []string{
		// A pasted block: fish runs it as one command line.
		"git pull\nmake build\nmem remember \"сборка\"",
		"for f in *.log\n    gzip $f\nend",
		"if test -f .env\n    source .env\nelse if test -f .env.example\n    cp .env.example .env\nelse\n    echo none\nend\nls -a",
		"function mkcd\n    mkdir -p $argv[1]; and cd $argv[1]\nend",
		"begin\n    echo a\n    echo b\nend > out.txt\ncat out.txt",
		"make test\nand make install\nor echo failed",
		"docker run \\\n    --rm alpine",
		"cat file |\n    grep error",
		"echo 'one\ntwo'\necho (date\n)",
		"switch $os\n    case Darwin\n        brew update\nend",
		"sleep 5 &\njobs",
		"# comment\necho x#y\n",
		"while true; sleep 1; end\necho done",
	}
	want := []string{
		"git pull",
		"make build",
		"for f in *.log\n    gzip $f\nend",
		"if test -f .env\n    source .env\nelse if test -f .env.example\n    cp .env.example .env\nelse\n    echo none\nend",
		"ls -a",
		"function mkcd\n    mkdir -p $argv[1]; and cd $argv[1]\nend",
		"begin\n    echo a\n    echo b\nend > out.txt",
		"cat out.txt",
		"make test\nand make install\nor echo failed",
		"docker run \\\n    --rm alpine",
		"cat file |\n    grep error",
		"echo 'one\ntwo'",
		"echo (date\n)",
		"switch $os\n    case Darwin\n        brew update\nend",
		"sleep 5 &",
		"jobs",
		"echo x#y",
		"while true; sleep 1; end",
		"echo done",
	}
	if got := Clean(in, "fish"); !reflect.DeepEqual(got, want) {
		t.Errorf("Clean =\n%q\nwant\n%q", got, want)
	}
}
