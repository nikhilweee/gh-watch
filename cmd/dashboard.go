package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/nikhilweee/gh-watch/internal/state"
	"github.com/nikhilweee/gh-watch/internal/tui"
)

type DashboardCmd struct{}

func (d *DashboardCmd) Run(cli *CLI) error {
	s, err := state.Load()
	if err != nil {
		return err
	}
	if len(s.Watches) == 0 {
		fmt.Println("No PRs being watched. Run `gh watch add <pr>` to add one.")
		return nil
	}
	m := tui.New(s.Watches)
	p := tea.NewProgram(m)
	_, err = p.Run()
	return err
}
