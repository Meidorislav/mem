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
	if _, err := shellInit("fish", "^G"); err == nil {
		t.Error("fish: want unsupported shell error")
	}

	for in, want := range map[string]string{"^G": `\C-g`, "^K": `\C-k`, "^[m": `\em`, `\C-x`: `\C-x`} {
		if got := bashKey(in); got != want {
			t.Errorf("bashKey(%q) = %q, want %q", in, got, want)
		}
	}
}
