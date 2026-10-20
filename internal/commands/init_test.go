package commands

import (
	"strings"
	"testing"
)

func TestShellInit(t *testing.T) {
	zsh, err := shellInit("zsh", "^G")
	if err != nil || !strings.Contains(zsh, "bindkey '^G' _mem_widget") || !strings.Contains(zsh, "mem ask --print") {
		t.Errorf("zsh init = %q, %v", zsh, err)
	}
	bash, err := shellInit("bash", "^[m")
	if err != nil || !strings.Contains(bash, `bind -x '"\em": _mem_widget'`) {
		t.Errorf("bash init = %q, %v", bash, err)
	}
	fish, err := shellInit("fish", "^G")
	if err != nil || !strings.Contains(fish, `bind \cg _mem_widget`) || !strings.Contains(fish, "bind -M insert \\cg _mem_widget") {
		t.Errorf("fish init = %q, %v", fish, err)
	}
	if _, err := shellInit("tcsh", "^G"); err == nil {
		t.Error("tcsh: want unsupported shell error")
	}

	for in, want := range map[string]string{"^G": `\C-g`, "^K": `\C-k`, "^[m": `\em`, `\C-x`: `\C-x`} {
		if got := bashKey(in); got != want {
			t.Errorf("bashKey(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"^G": `\cg`, "^[m": `\em`, "ctrl-g": "ctrl-g"} {
		if got := fishKey(in); got != want {
			t.Errorf("fishKey(%q) = %q, want %q", in, got, want)
		}
	}
}
