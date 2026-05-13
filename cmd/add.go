package cmd

import (
	"fmt"

	gh "github.com/nikhilweee/gh-watch/internal/github"
	"github.com/nikhilweee/gh-watch/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	addCmd.Flags().Bool("automerge", false, "Automatically merge when approved and CI passes")
}

var addCmd = &cobra.Command{
	Use:   "add <pr-number-or-url>",
	Short: "Add a PR to the watchlist",
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
		autoMerge, _ := cmd.Flags().GetBool("automerge")
		s.Add(prNum, repo, autoMerge)
		if err := s.Save(); err != nil {
			return err
		}

		msg := fmt.Sprintf("Watching PR #%d in %s.", prNum, repo)
		if autoMerge {
			msg += " Auto-merge enabled."
		}
		fmt.Println(msg + " Run `gh watch` to open the dashboard.")
		return nil
	},
}
