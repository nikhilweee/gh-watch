package cmd

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/nikhilweee/gh-watch/internal/state"
	"github.com/nikhilweee/gh-watch/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gh-watch [pr-number]",
	Short: "Watch PRs and auto-merge when ready",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return addCmd.RunE(addCmd, args)
		}

		s, err := state.Load()
		if err != nil {
			return err
		}

		if len(s.Watches) == 0 {
			fmt.Println("No PRs being watched. Run `gh watch <pr>` to add one.")
			return nil
		}

		m := tui.New(s.Watches)
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			return err
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("repo", "r", "", "Repository in owner/name format (defaults to current repo)")
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(listCmd)
}
