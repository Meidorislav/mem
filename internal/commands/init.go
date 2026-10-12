package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var initFlags struct {
	key string
}

var initCmd = &cobra.Command{
	Use:   "init <zsh|bash>",
	Short: "Print shell integration: a key that turns a question into a command",
	Long: `Print a shell widget bound to a key (Ctrl+G by default). Type a question on
the command line, press the key, and the line is replaced with the best
matching command from your memories, ready to edit or run.

Add to ~/.zshrc:    eval "$(mem init zsh)"
Add to ~/.bashrc:   eval "$(mem init bash)"

Pick another key with --key, in zsh notation: --key '^K', --key '^[m' (Alt+M).`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"zsh", "bash"},
	RunE: func(cmd *cobra.Command, args []string) error {
		script, err := shellInit(args[0], initFlags.key)
		if err != nil {
			return err
		}
		fmt.Print(script)
		return nil
	},
}

// shellInit returns the widget code for shell, bound to key (zsh notation:
// "^G" for Ctrl+G, "^[m" for Alt+M).
func shellInit(shell, key string) (string, error) {
	switch shell {
	case "zsh":
		return strings.ReplaceAll(zshInit, "@KEY@", key), nil
	case "bash":
		return strings.ReplaceAll(bashInit, "@KEY@", bashKey(key)), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: zsh, bash)", shell)
	}
}

// bashKey converts zsh key notation to readline's: "^G" -> "\C-g",
// "^[m" -> "\em". Anything else is passed through.
func bashKey(key string) string {
	switch {
	case strings.HasPrefix(key, "^[") && len(key) > 2:
		return `\e` + key[2:]
	case strings.HasPrefix(key, "^") && len(key) == 2:
		return `\C-` + strings.ToLower(key[1:])
	default:
		return key
	}
}

// Both widgets run `mem ask --print` with the terminal as stdin, so mem can
// ask which command to take when a memory has several; its prompts and
// errors go to stderr (the terminal) and only the command reaches stdout.
// On failure or cancel the line is left as it was.

const zshInit = `# mem shell integration: type a question, press the key, get the command.
_mem_widget() {
  emulate -L zsh
  if [[ -z $BUFFER ]]; then
    zle -M "mem: type a question first"
    return 1
  fi
  zle -I
  local cmd
  cmd=$(command mem ask --print -- "$BUFFER" </dev/tty)
  if [[ $? -eq 0 && -n $cmd ]]; then
    BUFFER=$cmd
    CURSOR=${#BUFFER}
  fi
  zle reset-prompt
}
zle -N _mem_widget
bindkey '@KEY@' _mem_widget
`

const bashInit = `# mem shell integration: type a question, press the key, get the command.
_mem_widget() {
  [[ -z $READLINE_LINE ]] && return
  local cmd
  cmd=$(command mem ask --print -- "$READLINE_LINE" </dev/tty) || return
  [[ -z $cmd ]] && return
  READLINE_LINE=$cmd
  READLINE_POINT=${#READLINE_LINE}
}
bind -x '"@KEY@": _mem_widget'
`

func init() {
	initCmd.Flags().StringVar(&initFlags.key, "key", "^G", "key to bind, in zsh notation (^G = Ctrl+G, ^[m = Alt+M)")
	rootCmd.AddCommand(initCmd)
}
