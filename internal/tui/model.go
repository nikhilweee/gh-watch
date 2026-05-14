package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/cli/go-gh/v2/pkg/browser"
	gh "github.com/nikhilweee/gh-watch/internal/github"
	"github.com/nikhilweee/gh-watch/internal/state"
)

const pollInterval = 60 * time.Second

type rowState int

const (
	rowPolling rowState = iota
	rowReady
	rowMerging
	rowMerged
	rowClosed
	rowError
)

type watchRow struct {
	watch  state.Watch
	result *gh.PollResult
	state  rowState
	err    error
	lastAt time.Time
}

type pollDoneMsg struct {
	repo   string
	pr     int
	result *gh.PollResult
	err    error
}

type mergeDoneMsg struct {
	repo string
	pr   int
	err  error
}

type tickMsg time.Time

type Model struct {
	rows     []watchRow
	cursor   int
	spinner  spinner.Model
	input    textinput.Model
	adding   bool
	inputErr string
	width    int
	quitting bool
}

func New(watches []state.Watch) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	ti := textinput.New()
	ti.CharLimit = 200

	rows := make([]watchRow, len(watches))
	for i, w := range watches {
		rows[i] = watchRow{watch: w, state: rowPolling}
	}
	return Model{rows: rows, spinner: s, input: ti}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) }),
	}
	for i := range m.rows {
		cmds = append(cmds, pollCmd(m.rows[i].watch))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width

	case tea.KeyPressMsg:
		// Input mode: intercept keys for the text field
		if m.adding {
			switch msg.String() {
			case "esc":
				m.adding = false
				m.inputErr = ""
				m.input.SetValue("")
				return m, nil
			case "enter":
				val := strings.TrimSpace(m.input.Value())
				if val == "" {
					m.adding = false
					return m, nil
				}
				repo, prNum, err := gh.ResolveRepo(val, "")
				if err != nil {
					m.inputErr = err.Error()
					return m, nil
				}
				w := state.Watch{PR: prNum, Repo: repo}
				m.rows = append(m.rows, watchRow{watch: w, state: rowPolling})
				m.adding = false
				m.inputErr = ""
				m.input.SetValue("")
				return m, tea.Batch(addToState(w), pollCmd(w))
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.inputErr = ""
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "a":
			m.adding = true
			m.inputErr = ""
			m.input.SetValue("")
			return m, m.input.Focus()
		case "r":
			var cmds []tea.Cmd
			for i := range m.rows {
				if m.rows[i].state != rowMerging {
					m.rows[i].state = rowPolling
					cmds = append(cmds, pollCmd(m.rows[i].watch))
				}
			}
			return m, tea.Batch(cmds...)
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "x":
			if len(m.rows) > 0 {
				w := m.rows[m.cursor].watch
				m.rows = append(m.rows[:m.cursor], m.rows[m.cursor+1:]...)
				if m.cursor >= len(m.rows) && m.cursor > 0 {
					m.cursor--
				}
				return m, removeFromState(w)
			}
		case "o":
			if len(m.rows) > 0 {
				w := m.rows[m.cursor].watch
				return m, openInBrowserCmd(w)
			}
		case "m":
			if len(m.rows) > 0 {
				m.rows[m.cursor].watch.AutoMerge = !m.rows[m.cursor].watch.AutoMerge
				w := m.rows[m.cursor].watch
				cmds := []tea.Cmd{setAutoMergeInState(w)}
				if w.AutoMerge && m.rows[m.cursor].result != nil && m.rows[m.cursor].result.IsReady() {
					m.rows[m.cursor].state = rowMerging
					cmds = append(cmds, mergeCmd(w, m.rows[m.cursor].result.PR.NodeID))
				}
				return m, tea.Batch(cmds...)
			}
		}

	default:
		if m.adding {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		cmds := []tea.Cmd{
			tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) }),
		}
		for i := range m.rows {
			if m.rows[i].state != rowMerging && m.rows[i].state != rowMerged && m.rows[i].state != rowClosed {
				m.rows[i].state = rowPolling
				cmds = append(cmds, pollCmd(m.rows[i].watch))
			}
		}
		return m, tea.Batch(cmds...)

	case pollDoneMsg:
		for i := range m.rows {
			w := m.rows[i].watch
			if w.PR != msg.pr || w.Repo != msg.repo {
				continue
			}
			m.rows[i].lastAt = time.Now()
			if msg.err != nil {
				m.rows[i].state = rowError
				m.rows[i].err = msg.err
				break
			}
			m.rows[i].result = msg.result
			switch msg.result.PR.State {
			case "MERGED":
				m.rows[i].state = rowMerged
				break
			case "CLOSED":
				m.rows[i].state = rowClosed
				break
			default:
				if msg.result.IsReady() && w.AutoMerge {
					m.rows[i].state = rowMerging
					return m, mergeCmd(w, msg.result.PR.NodeID)
				}
				m.rows[i].state = rowReady
			}
			break
		}

	case mergeDoneMsg:
		for i := range m.rows {
			w := m.rows[i].watch
			if w.PR != msg.pr || w.Repo != msg.repo {
				continue
			}
			if msg.err != nil {
				m.rows[i].state = rowError
				m.rows[i].err = msg.err
			} else {
				m.rows[i].state = rowMerged
			}
			break
		}
	}

	return m, nil
}

func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var inner strings.Builder

	if len(m.rows) == 0 {
		inner.WriteString(styleMuted.Render("No PRs being watched. Press 'a' to add one."))
	} else {
		tbl := table.New().
			Border(lipgloss.HiddenBorder()).
			BorderTop(false).BorderBottom(false).
			BorderLeft(false).BorderRight(false).
			BorderColumn(false).BorderRow(false).BorderHeader(false).
			Headers("PR", "STATUS", "AUTO", "TITLE", "REVIEWS", "CHECKS", "UPDATED").
			StyleFunc(func(row, col int) lipgloss.Style {
				s := lipgloss.NewStyle().Padding(0, 1)
				if row == table.HeaderRow {
					return s.Bold(true).Faint(true).Align(lipgloss.Center)
				}
				if row == m.cursor {
					s = s.Background(selectedBg)
				}
				switch col {
				case 0:
					s = s.Bold(true)
				case 1, 4, 5, 6:
					s = s.Align(lipgloss.Center)
				}
				return s
			})
		for i, row := range m.rows {
			tbl = tbl.Row(m.rowCells(row, i == m.cursor)...)
		}
		inner.WriteString(tbl.Render())
	}

	// footer
	var footer strings.Builder
	if m.adding {
		hint := ""
		if m.input.Value() == "" {
			hint = styleMuted.Render("PR number or GitHub URL")
		}
		footer.WriteString(styleBold.Render("Add: ") + m.input.View() + hint)
		if m.inputErr != "" {
			footer.WriteString("  " + styleRed.Render(m.inputErr))
		}
		footer.WriteString("\n" + styleHelp.Render("[enter] confirm  [esc] cancel"))
	} else {
		footer.WriteString(styleHelp.Render("[↑/↓] navigate  [a] add  [x] remove  [m] auto-merge  [r] refresh   [o] open in browser   [q] quit"))
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("gh watch") + "\n\n")
	b.WriteString(inner.String())
	b.WriteString("\n\n")
	b.WriteString(footer.String())

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// rowCells returns the 7 cells for a watch row in column order:
// PR, STATUS, AUTO, TITLE, REVIEWS, CHECKS, UPDATED.
func (m Model) rowCells(row watchRow, selected bool) []string {
	prNum := fmt.Sprintf("%d", row.watch.PR)

	var status, auto, title, reviews, checks, updated string

	switch row.state {
	case rowMerged:
		prNum = styleMuted.Render(prNum)
		status = styleMuted.Render("merged")
		if row.result != nil {
			r := row.result
			title = styleMuted.Render(truncate(r.PR.Title, m.titleMaxWidth()))
			reviews = styleMuted.Render(fmt.Sprintf("%d/%d/%d", r.ApprovedReviews, r.PendingReviews, r.ChangesReviews))
			checks = styleMuted.Render(fmt.Sprintf("%d/%d/%d", r.PassedChecks, r.RunningChecks, r.FailedChecks))
			if !row.lastAt.IsZero() {
				updated = styleMuted.Render(formatDuration(time.Since(row.lastAt)))
			}
		}

	case rowClosed:
		prNum = styleMuted.Render(prNum)
		status = styleMuted.Render("closed")
		if row.result != nil {
			r := row.result
			title = styleMuted.Render(truncate(r.PR.Title, m.titleMaxWidth()))
			reviews = styleMuted.Render(fmt.Sprintf("%d/%d/%d", r.ApprovedReviews, r.PendingReviews, r.ChangesReviews))
			checks = styleMuted.Render(fmt.Sprintf("%d/%d/%d", r.PassedChecks, r.RunningChecks, r.FailedChecks))
			if !row.lastAt.IsZero() {
				updated = styleMuted.Render(formatDuration(time.Since(row.lastAt)))
			}
		}

	case rowPolling, rowReady, rowMerging:
		if row.result == nil {
			if row.state == rowPolling {
				title = m.spinner.View()
			}
			break
		}
		status = readinessLabel(row.result, selected)
		if row.watch.AutoMerge {
			auto = styleYellow.Render("auto")
		}
		title = truncate(row.result.PR.Title, m.titleMaxWidth())
		reviews = reviewsLabel(row.result, selected)
		if row.state == rowMerging {
			checks = styleYellow.Render("merging " + m.spinner.View())
		} else {
			checks = checksLabel(row.result, selected)
		}
		switch {
		case row.state == rowPolling:
			updated = m.spinner.View()
		case !row.lastAt.IsZero():
			updated = styleMuted.Render(formatDuration(time.Since(row.lastAt)))
		}

	case rowError:
		status = colorDot(styleRed, selected)
		if row.err != nil {
			title = styleMuted.Render(row.err.Error())
		}
	}

	return []string{prNum, status, auto, title, reviews, checks, updated}
}

func (m Model) titleMaxWidth() int {
	if m.width <= 0 {
		return 60
	}
	// rough budget for the other 6 columns + table padding (2 chars per side per col)
	max := m.width - 70
	if max < 20 {
		max = 20
	}
	return max
}

func readinessLabel(r *gh.PollResult, selected bool) string {
	colorWord := func(style lipgloss.Style, word string) string {
		if selected {
			style = style.Background(selectedBg)
		}
		return style.Render(word)
	}
	switch {
	case needsUserAction(r):
		return colorWord(styleRed, "action")
	case r.IsReady():
		return colorWord(styleGreen, "ready")
	default:
		return colorWord(styleYellow, "pending")
	}
}

func needsUserAction(r *gh.PollResult) bool {
	if r.PR.ReviewDecision == "CHANGES_REQUESTED" || r.ChangesReviews > 0 {
		return true
	}
	if r.ChecksState == "FAILURE" || r.ChecksState == "ERROR" {
		return true
	}
	if r.PR.MergeStateStatus == "DIRTY" {
		return true
	}
	return false
}

func colorDot(style lipgloss.Style, selected bool) string {
	if selected {
		style = style.Background(selectedBg)
	}
	return style.Render("●")
}

var selectedBg = lipgloss.Color("237")

func reviewsLabel(r *gh.PollResult, selected bool) string {
	sep := sepWithBg(selected)
	return coloredCount(r.ApprovedReviews, styleGreen, selected) + sep +
		coloredCount(r.PendingReviews, styleYellow, selected) + sep +
		coloredCount(r.ChangesReviews, styleRed, selected)
}

func checksLabel(r *gh.PollResult, selected bool) string {
	sep := sepWithBg(selected)
	return coloredCount(r.PassedChecks, styleGreen, selected) + sep +
		coloredCount(r.RunningChecks, styleYellow, selected) + sep +
		coloredCount(r.FailedChecks, styleRed, selected)
}

func sepWithBg(selected bool) string {
	if !selected {
		return "/"
	}
	return lipgloss.NewStyle().Background(selectedBg).Render("/")
}

func coloredCount(n int, style lipgloss.Style, selected bool) string {
	s := fmt.Sprintf("%d", n)
	base := style
	if n == 0 {
		base = styleMuted
	}
	if selected {
		base = base.Background(selectedBg)
	}
	return base.Render(s)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max < 1 {
		return ""
	}
	return string(runes[:max-1]) + "…"
}

func pollCmd(w state.Watch) tea.Cmd {
	return func() tea.Msg {
		result, err := gh.GetPRStatus(w.Repo, w.PR)
		return pollDoneMsg{repo: w.Repo, pr: w.PR, result: result, err: err}
	}
}

func mergeCmd(w state.Watch, nodeID string) tea.Cmd {
	return func() tea.Msg {
		err := gh.MergePR(nodeID)
		return mergeDoneMsg{repo: w.Repo, pr: w.PR, err: err}
	}
}

func openInBrowserCmd(w state.Watch) tea.Cmd {
	return func() tea.Msg {
		url := fmt.Sprintf("https://github.com/%s/pull/%d", w.Repo, w.PR)
		_ = browser.New("", os.Stdout, os.Stderr).Browse(url)
		return nil
	}
}

func mutateState(fn func(*state.State)) tea.Cmd {
	return func() tea.Msg {
		s, err := state.Load()
		if err != nil {
			return nil
		}
		fn(s)
		_ = s.Save()
		return nil
	}
}

func addToState(w state.Watch) tea.Cmd {
	return mutateState(func(s *state.State) { s.Add(w.PR, w.Repo, w.AutoMerge) })
}

func removeFromState(w state.Watch) tea.Cmd {
	return mutateState(func(s *state.State) { s.Remove(w.PR, w.Repo) })
}

func setAutoMergeInState(w state.Watch) tea.Cmd {
	return mutateState(func(s *state.State) { s.SetAutoMerge(w.PR, w.Repo, w.AutoMerge) })
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
