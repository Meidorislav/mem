package commands

import (
	"fmt"
	"os"

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

var saveCmd = &cobra.Command{
	Use:   "save [title]",
	Short: "Capture a new memory",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Saving memory: %s\n", args[0])
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

func init() {
	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(watchCmd)
}
