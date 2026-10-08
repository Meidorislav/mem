package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/meidori/mem/internal/session"
	"github.com/meidori/mem/internal/storage"
	"github.com/spf13/cobra"
)

var watchFlags struct {
	shell string
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Record terminal session",
	Long: `Start a recorded subshell. Every command you run is logged until you exit;
then turn the session into a memory with ` + "`mem remember \"title\"`" + ` (also works
from inside the watched shell). Commands starting with a space are skipped
when your shell ignores them for history (HISTCONTROL=ignorespace in bash,
setopt HIST_IGNORE_SPACE in zsh).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if path := os.Getenv(session.EnvVar); path != "" {
			return fmt.Errorf("already inside a mem watch session (%s); type `exit` to end it", path)
		}

		dir, err := session.Dir()
		if err != nil {
			return err
		}
		path, err := session.Create(dir)
		if err != nil {
			return err
		}

		sh, err := session.NewShell(watchFlags.shell, path)
		if err != nil {
			os.Remove(path)
			return err
		}
		defer sh.Cleanup()

		// The shell owns the terminal; keep Ctrl-C and friends from killing
		// mem while it waits. Caught (not ignored) signals are reset to
		// default in the child, so Ctrl-C still works inside the shell.
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGQUIT)
		defer signal.Stop(sigs)

		fmt.Printf("Recording %s session. Run `mem remember \"title\"` to save it, `exit` to stop.\n", sh.Name)
		if err := sh.Cmd().Run(); err != nil {
			// A non-zero exit just reflects the last command in the shell.
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				return fmt.Errorf("running shell: %w", err)
			}
		}

		raw, err := session.Read(path)
		if err != nil && !errors.Is(err, session.ErrNoSession) {
			return err
		}
		cmds := session.Clean(raw)
		if len(cmds) == 0 {
			os.Remove(path)
			fmt.Println("Session ended; nothing left to remember.")
			return nil
		}

		fmt.Printf("Session ended with %d command(s). Save it with `mem remember \"title\"`.\n", len(cmds))
		return nil
	},
}

var rememberFlags struct {
	tags        []string
	description string
	last        int
	force       bool
}

var rememberCmd = &cobra.Command{
	Use:   "remember [title]",
	Short: "Transform terminal history into a memory",
	Long: `Save the commands recorded by ` + "`mem watch`" + ` as a memory.

Inside a watched shell this saves everything since the session started (or
since the previous ` + "`mem remember`" + `). Outside, it uses the most recently
finished session and removes it once saved.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, inside := os.LookupEnv(session.EnvVar)
		if !inside || path == "" {
			inside = false
			dir, err := session.Dir()
			if err != nil {
				return err
			}
			if path, err = session.Latest(dir); err != nil {
				return err
			}
		}

		raw, err := session.Read(path)
		if err != nil {
			return err
		}
		cmds := session.Clean(raw)
		if n := rememberFlags.last; n > 0 && len(cmds) > n {
			cmds = cmds[len(cmds)-n:]
		}
		if len(cmds) == 0 {
			return errors.New("no commands recorded in this session yet")
		}

		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		title := strings.Join(args, " ")
		m := &storage.Memory{
			Title:       title,
			Source:      storage.SourceRemember,
			Description: rememberFlags.description,
			Tags:        rememberFlags.tags,
		}
		for _, c := range cmds {
			m.Commands = append(m.Commands, storage.Command{Command: c})
		}

		if !rememberFlags.force {
			if err := checkDuplicate(store, m); err != nil {
				return err
			}
		}

		if err := store.SaveMemory(m); err != nil {
			return fmt.Errorf("saving memory: %w", err)
		}

		// Consume the session: a live one starts over, a finished one is done.
		if inside {
			err = os.Truncate(path, 0)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not clear session file %s: %v\n", path, err)
		}

		fmt.Printf("Remembered #%d: %s\n", m.ID, title)
		for _, c := range m.Commands {
			fmt.Printf("  $ %s\n", c.Command)
		}
		warnIfNotIndexed(store, m)
		return nil
	},
}

func init() {
	watchCmd.Flags().StringVar(&watchFlags.shell, "shell", "", "shell to record (bash or zsh; default $SHELL)")

	rememberCmd.Flags().StringArrayVarP(&rememberFlags.tags, "tag", "t", nil, "tag to assign")
	rememberCmd.Flags().StringVarP(&rememberFlags.description, "description", "d", "", "longer description of the memory")
	rememberCmd.Flags().IntVarP(&rememberFlags.last, "last", "n", 0, "keep only the last N commands")
	rememberCmd.Flags().BoolVar(&rememberFlags.force, "force", false, "save even if an identical memory exists")
}
