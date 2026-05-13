package cmd

import "github.com/alecthomas/kong"

type CLI struct {
	Repo string `short:"r" help:"Repository in owner/name format (defaults to current repo)" placeholder:"OWNER/NAME"`

	Add    AddCmd    `cmd:"" help:"Add a PR to the watchlist"`
	Remove RemoveCmd `cmd:"" help:"Remove a PR from the watchlist"`
	List   ListCmd   `cmd:"" help:"List all watched PRs"`

	Dashboard DashboardCmd `cmd:"" default:"withargs" hidden:""`
}

func Execute() {
	var cli CLI
	ctx := kong.Parse(
		&cli,
		kong.Name("gh watch"),
		kong.Description("Watch PRs and auto-merge when ready."),
		kong.UsageOnError(),
		kong.Bind(&cli),
	)
	ctx.FatalIfErrorf(ctx.Run())
}
