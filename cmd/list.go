package cmd

import (
	"fmt"

	"github.com/nikhilweee/gh-watch/internal/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all watched PRs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := state.Load()
		if err != nil {
			return err
		}
		if len(s.Watches) == 0 {
			fmt.Println("No PRs being watched.")
			return nil
		}
		for _, w := range s.Watches {
			fmt.Printf("  #%d  %s\n", w.PR, w.Repo)
		}
		return nil
	},
}
