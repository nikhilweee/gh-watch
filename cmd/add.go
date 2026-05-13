package cmd

import (
	"fmt"

	gh "github.com/nikhilweee/gh-watch/internal/github"
	"github.com/nikhilweee/gh-watch/internal/state"
)

type AddCmd struct {
	PR        string `arg:"" name:"pr" help:"PR number or GitHub PR URL"`
	AutoMerge bool   `name:"automerge" help:"Automatically merge when approved and CI passes"`
}

func (a *AddCmd) Run(cli *CLI) error {
	repo, prNum, err := gh.ResolveRepo(a.PR, cli.Repo)
	if err != nil {
		return err
	}
	s, err := state.Load()
	if err != nil {
		return err
	}
	s.Add(prNum, repo, a.AutoMerge)
	if err := s.Save(); err != nil {
		return err
	}
	msg := fmt.Sprintf("Watching PR #%d in %s.", prNum, repo)
	if a.AutoMerge {
		msg += " Auto-merge enabled."
	}
	fmt.Println(msg + " Run `gh watch` to open the dashboard.")
	return nil
}
