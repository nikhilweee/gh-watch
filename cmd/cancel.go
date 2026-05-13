package cmd

import (
	"fmt"

	gh "github.com/nikhilweee/gh-watch/internal/github"
	"github.com/nikhilweee/gh-watch/internal/state"
	"github.com/spf13/cobra"
)

var cancelCmd = &cobra.Command{
	Use:   "cancel <pr-number-or-url>",
	Short: "Remove a PR from the watchlist",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoFlag, _ := cmd.Flags().GetString("repo")
		repo, prNum, err := gh.ResolveRepo(args[0], repoFlag)
		if err != nil {
			return err
		}

		s, err := state.Load()
		if err != nil {
			return err
		}
		s.Remove(prNum, repo)
		if err := s.Save(); err != nil {
			return err
		}

		fmt.Printf("Stopped watching PR #%d in %s.\n", prNum, repo)
		return nil
	},
}
