package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/meidori/mem/internal/storage"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mem",
	Short: "MEM is your local technical oracle",
	Long:  `A CLI-first "second brain" that captures your terminal sessions, notes, and configs, making them searchable through local AI.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var saveFlags struct {
	commands []string
	tags     []string
}

var saveCmd = &cobra.Command{
	Use:   "save [title]",
	Short: "Capture a new memory",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewStore()
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		title := strings.Join(args, " ")
		m := &storage.Memory{
			Title:  title,
			Source: storage.SourceSave,
			Tags:   saveFlags.tags,
		}
		for _, c := range saveFlags.commands {
			m.Commands = append(m.Commands, storage.Command{Command: c})
		}

		if err := store.SaveMemory(m); err != nil {
			return fmt.Errorf("saving memory: %w", err)
		}

		fmt.Printf("Saved: %s\n", title)
		return nil
	},
}

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Search using natural language",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Asking AI: %s\n", args[0])
	},
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Record terminal session",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Watching terminal...")
	},
}

var rememberCmd = &cobra.Command{
	Use:   "remember [description]",
	Short: "Transform terminal history into a memory",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Remembering session: %s\n", args[0])
	},
}

func init() {
	saveCmd.Flags().StringArrayVarP(&saveFlags.commands, "command", "c", nil, "shell command to attach")
	saveCmd.Flags().StringArrayVarP(&saveFlags.tags, "tag", "t", nil, "tag to assign")

	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(rememberCmd)
}
