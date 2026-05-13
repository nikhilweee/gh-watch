package cmd

import (
	"fmt"

	gh "github.com/nikhilweee/gh-watch/internal/github"
	"github.com/nikhilweee/gh-watch/internal/state"
)

type RemoveCmd struct {
	PR string `arg:"" name:"pr" help:"PR number or GitHub PR URL"`
}

func (c *RemoveCmd) Run(cli *CLI) error {
	repo, prNum, err := gh.ResolveRepo(c.PR, cli.Repo)
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
}
