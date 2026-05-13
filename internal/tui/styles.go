package tui

import "charm.land/lipgloss/v2"

var (
	styleBold  = lipgloss.NewStyle().Bold(true)
	styleGreen    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleRed      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleYellow   = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	styleBlue     = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	styleMuted    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleHelp     = lipgloss.NewStyle().Faint(true)
)
