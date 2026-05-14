package tui

import (
	"fmt"
	"os"
	"sort"
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

type uiMode int

const (
	modeNormal uiMode = iota
	modeAdding
	modePicking
	modeSorting
)

type columnDef struct {
	id       string
	header   string
	center   bool
	sortable bool
}

var allColumns = []columnDef{
	{"PR", "PR", true, true},
	{"STATUS", "STATUS", true, false},
	{"AUTO", "AUTO", true, false},
	{"TITLE", "TITLE", false, true},
	{"AUTHOR", "AUTHOR", false, true},
	{"BASE", "BASE", false, true},
	{"DRAFT", "DRAFT", true, false},
	{"REVIEWS", "REVIEWS", true, true},
	{"CHECKS", "CHECKS", true, true},
	{"UPDATED", "UPDATED", true, true},
}

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
	rows         []watchRow
	cursor       int
	spinner      spinner.Model
	input        textinput.Model
	mode         uiMode
	inputErr     string
	width        int
	quitting     bool
	columns      []columnDef
	sortBy       string
	sortDesc     bool
	pickerDraft  []string
	pickerCursor int
}

func columnsFromIDs(ids []string) []columnDef {
	active := map[string]bool{"PR": true} // PR is always visible
	for _, id := range ids {
		active[id] = true
	}
	var cols []columnDef
	for _, c := range allColumns {
		if active[c.id] {
			cols = append(cols, c)
		}
	}
	return cols
}

func New(watches []state.Watch) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	ti := textinput.New()
	ti.CharLimit = 200

	cfg, _ := state.LoadConfig()

	rows := make([]watchRow, len(watches))
	for i, w := range watches {
		rows[i] = watchRow{watch: w, state: rowPolling}
	}
	return Model{
		rows:     rows,
		spinner:  s,
		input:    ti,
		columns:  columnsFromIDs(cfg.Columns),
		sortBy:   cfg.SortBy,
		sortDesc: cfg.SortDesc,
	}
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
		switch m.mode {
		case modeAdding:
			switch msg.String() {
			case "esc":
				m.mode = modeNormal
				m.inputErr = ""
				m.input.SetValue("")
				return m, nil
			case "enter":
				val := strings.TrimSpace(m.input.Value())
				if val == "" {
					m.mode = modeNormal
					return m, nil
				}
				repo, prNum, err := gh.ResolveRepo(val, "")
				if err != nil {
					m.inputErr = err.Error()
					return m, nil
				}
				w := state.Watch{PR: prNum, Repo: repo}
				m.rows = append(m.rows, watchRow{watch: w, state: rowPolling})
				m.mode = modeNormal
				m.inputErr = ""
				m.input.SetValue("")
				return m, tea.Batch(addToState(w), pollCmd(w))
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.inputErr = ""
				return m, cmd
			}

		case modePicking:
			switch msg.String() {
			case "esc":
				m.mode = modeNormal
				return m, nil
			case "enter":
				m.columns = columnsFromIDs(m.pickerDraft)
				m.mode = modeNormal
				cfg := state.Config{Columns: m.pickerDraft, SortBy: m.sortBy, SortDesc: m.sortDesc}
				return m, saveConfigCmd(cfg)
			case "up", "k":
				if m.pickerCursor > 0 {
					m.pickerCursor--
				}
			case "down", "j":
				if m.pickerCursor < len(allColumns)-1 {
					m.pickerCursor++
				}
			case "space":
				id := allColumns[m.pickerCursor].id
				if id == "PR" {
					break
				}
				found := false
				for i, v := range m.pickerDraft {
					if v == id {
						m.pickerDraft = append(m.pickerDraft[:i], m.pickerDraft[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					m.pickerDraft = append(m.pickerDraft, id)
				}
			}
			return m, nil

		case modeSorting:
			sortable := sortableColumns()
			switch msg.String() {
			case "esc", "enter":
				m.mode = modeNormal
				return m, nil
			case "up", "k":
				if m.pickerCursor > 0 {
					m.pickerCursor--
				}
			case "down", "j":
				if m.pickerCursor < len(sortable) {
					m.pickerCursor++
				}
			case "space":
				var id string
				if m.pickerCursor > 0 {
					id = sortable[m.pickerCursor-1].id
				}
				if id == m.sortBy && id != "" {
					m.sortDesc = !m.sortDesc
				} else {
					m.sortBy = id
					m.sortDesc = false
				}
				m.sortRows()
				cfg := state.Config{Columns: columnIDs(m.columns), SortBy: m.sortBy, SortDesc: m.sortDesc}
				return m, saveConfigCmd(cfg)
			}
			return m, nil

		default: // modeNormal
			switch msg.String() {
			case "q", "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			case "a":
				m.mode = modeAdding
				m.inputErr = ""
				m.input.SetValue("")
				return m, m.input.Focus()
			case "c":
				m.mode = modePicking
				m.pickerDraft = columnIDs(m.columns)
				m.pickerCursor = 0
				return m, nil
			case "s":
				m.mode = modeSorting
				m.pickerCursor = sortCursorFor(m.sortBy)
				return m, nil
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
		}

	default:
		if m.mode == modeAdding {
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
			case "CLOSED":
				m.rows[i].state = rowClosed
			default:
				if msg.result.IsReady() && w.AutoMerge {
					m.rows[i].state = rowMerging
					return m, mergeCmd(w, msg.result.PR.NodeID)
				}
				m.rows[i].state = rowReady
			}
			m.sortRows()
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

	switch m.mode {
	case modePicking:
		inner.WriteString(styleBold.Render("Select columns") + "\n\n")
		activeSet := map[string]bool{}
		for _, id := range m.pickerDraft {
			activeSet[id] = true
		}
		for i, c := range allColumns {
			locked := c.id == "PR"
			cursor := "  "
			if i == m.pickerCursor {
				cursor = styleGreen.Render("▶ ")
			}
			var check, label string
			if locked {
				check = styleMuted.Render("[x]")
				label = styleMuted.Render(c.header)
			} else if activeSet[c.id] {
				check = styleGreen.Render("[x]")
				label = c.header
			} else {
				check = styleMuted.Render("[ ]")
				label = c.header
			}
			inner.WriteString(fmt.Sprintf("%s%s  %s\n", cursor, check, label))
		}

	case modeSorting:
		inner.WriteString(styleBold.Render("Sort by") + "\n\n")
		sortable := sortableColumns()
		visibleCols := map[string]bool{}
		for _, c := range m.columns {
			visibleCols[c.id] = true
		}
		entries := append([]string{"None"}, func() []string {
			names := make([]string, len(sortable))
			for i, c := range sortable {
				names[i] = c.header
			}
			return names
		}()...)
		for i, name := range entries {
			cursor := "  "
			if i == m.pickerCursor {
				cursor = styleGreen.Render("▶ ")
			}
			var id string
			if i > 0 {
				id = sortable[i-1].id
			}
			hidden := id != "" && !visibleCols[id]
			label := name
			if id == m.sortBy && m.sortBy != "" {
				if m.sortDesc {
					label += " ↓"
				} else {
					label += " ↑"
				}
				label = styleGreen.Render(label)
			} else if hidden {
				label = styleMuted.Render(label)
			}
			inner.WriteString(fmt.Sprintf("%s%s\n", cursor, label))
		}

	default:
		if len(m.rows) == 0 {
			inner.WriteString(styleMuted.Render("No PRs being watched. Press 'a' to add one."))
		} else {
			headers := make([]string, len(m.columns))
			for i, c := range m.columns {
				h := c.header
				if c.id == m.sortBy && m.sortBy != "" {
					if m.sortDesc {
						h += " ↓"
					} else {
						h += " ↑"
					}
				}
				headers[i] = h
			}
			tbl := table.New().
				Border(lipgloss.HiddenBorder()).
				BorderTop(false).BorderBottom(false).
				BorderLeft(false).BorderRight(false).
				BorderColumn(false).BorderRow(false).BorderHeader(false).
				Headers(headers...).
				StyleFunc(func(row, col int) lipgloss.Style {
					s := lipgloss.NewStyle().Padding(0, 1)
					if row == table.HeaderRow {
						return s.Bold(true).Faint(true).Align(lipgloss.Center)
					}
					if row == m.cursor {
						s = s.Background(selectedBg)
					}
					if col >= 0 && col < len(m.columns) {
						c := m.columns[col]
						if c.id == "PR" {
							s = s.Bold(true)
						}
						if c.center {
							s = s.Align(lipgloss.Center)
						}
					}
					return s
				})
			for i, row := range m.rows {
				cells := make([]string, len(m.columns))
				for j, col := range m.columns {
					cells[j] = m.cellValue(row, col, i == m.cursor)
				}
				tbl = tbl.Row(cells...)
			}
			inner.WriteString(tbl.Render())
		}
	}

	// footer
	var footer strings.Builder
	switch m.mode {
	case modeAdding:
		hint := ""
		if m.input.Value() == "" {
			hint = styleMuted.Render("PR number or GitHub URL")
		}
		footer.WriteString(styleBold.Render("Add: ") + m.input.View() + hint)
		if m.inputErr != "" {
			footer.WriteString("  " + styleRed.Render(m.inputErr))
		}
		footer.WriteString("\n" + styleHelp.Render("[enter] confirm  [esc] cancel"))
	case modePicking:
		footer.WriteString(styleHelp.Render("[space] toggle  [enter] confirm  [esc] cancel"))
	case modeSorting:
		footer.WriteString(styleHelp.Render("[space] sort · space again to reverse  [enter/esc] close"))
	default:
		footer.WriteString(styleHelp.Render("[↑/↓] navigate  [a] add  [x] remove  [m] auto-merge  [r] refresh  [c] columns  [s] sort  [o] open  [q] quit"))
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

func (m Model) cellValue(row watchRow, col columnDef, selected bool) string {
	prevMerged := row.result != nil && row.result.PR.State == "MERGED"
	prevClosed := row.result != nil && row.result.PR.State == "CLOSED"
	dim := row.state == rowMerged || row.state == rowClosed ||
		(row.state == rowPolling && (prevMerged || prevClosed))
	r := row.result

	switch col.id {
	case "PR":
		s := fmt.Sprintf("%d", row.watch.PR)
		if row.state == rowPolling {
			return m.spinner.View()
		}
		if dim {
			return styleMuted.Render(s)
		}
		return s

	case "STATUS":
		if dim {
			if row.state == rowMerged || prevMerged {
				return styleMuted.Render("merged")
			}
			return styleMuted.Render("closed")
		}
		if row.state == rowError {
			return styleRed.Render("error")
		}
		if r == nil {
			return ""
		}
		return readinessLabel(r, selected)

	case "AUTO":
		if dim || !row.watch.AutoMerge {
			return ""
		}
		return styleYellow.Render("auto")

	case "TITLE":
		if row.state == rowError && row.err != nil {
			return styleMuted.Render(row.err.Error())
		}
		if r == nil {
			if row.state == rowPolling {
				return m.spinner.View()
			}
			return ""
		}
		title := truncate(r.PR.Title, m.titleMaxWidth())
		if dim {
			return styleMuted.Render(title)
		}
		return title

	case "AUTHOR":
		if r == nil {
			return ""
		}
		if dim {
			return styleMuted.Render(r.PR.Author)
		}
		return r.PR.Author

	case "BASE":
		if r == nil {
			return ""
		}
		if dim {
			return styleMuted.Render(r.PR.BaseRefName)
		}
		return r.PR.BaseRefName

	case "DRAFT":
		if r == nil || !r.PR.IsDraft {
			return ""
		}
		if dim {
			return styleMuted.Render("draft")
		}
		return styleYellow.Render("draft")

	case "REVIEWS":
		if r == nil {
			return ""
		}
		if dim {
			return styleMuted.Render(fmt.Sprintf("%d/%d/%d", r.ApprovedReviews, r.PendingReviews, r.ChangesReviews))
		}
		return reviewsLabel(r, selected)

	case "CHECKS":
		if r == nil {
			return ""
		}
		if row.state == rowMerging {
			return styleYellow.Render("merging " + m.spinner.View())
		}
		if dim {
			return styleMuted.Render(fmt.Sprintf("%d/%d/%d", r.PassedChecks, r.RunningChecks, r.FailedChecks))
		}
		return checksLabel(r, selected)

	case "UPDATED":
		if row.lastAt.IsZero() {
			if row.state == rowPolling {
				return m.spinner.View()
			}
			return ""
		}
		return styleMuted.Render(formatDuration(time.Since(row.lastAt)))
	}

	return ""
}

func (m *Model) sortRows() {
	if m.sortBy == "" {
		return
	}
	sort.SliceStable(m.rows, func(i, j int) bool {
		iDone := m.rows[i].state == rowMerged || m.rows[i].state == rowClosed
		jDone := m.rows[j].state == rowMerged || m.rows[j].state == rowClosed
		if iDone != jDone {
			return !iDone
		}
		ri := m.rows[i].result
		rj := m.rows[j].result
		if ri == nil || rj == nil {
			return ri != nil
		}
		var less bool
		switch m.sortBy {
		case "PR":
			less = m.rows[i].watch.PR < m.rows[j].watch.PR
		case "TITLE":
			less = ri.PR.Title < rj.PR.Title
		case "AUTHOR":
			less = ri.PR.Author < rj.PR.Author
		case "BASE":
			less = ri.PR.BaseRefName < rj.PR.BaseRefName
		case "REVIEWS":
			less = ri.ApprovedReviews < rj.ApprovedReviews
		case "CHECKS":
			less = ri.PassedChecks < rj.PassedChecks
		case "UPDATED":
			less = m.rows[i].lastAt.Before(m.rows[j].lastAt)
		}
		if m.sortDesc {
			return !less
		}
		return less
	})
}

func sortableColumns() []columnDef {
	var out []columnDef
	for _, c := range allColumns {
		if c.sortable {
			out = append(out, c)
		}
	}
	return out
}

func sortCursorFor(sortBy string) int {
	if sortBy == "" {
		return 0
	}
	for i, c := range sortableColumns() {
		if c.id == sortBy {
			return i + 1
		}
	}
	return 0
}

func columnIDs(cols []columnDef) []string {
	ids := make([]string, len(cols))
	for i, c := range cols {
		ids[i] = c.id
	}
	return ids
}

func (m Model) titleMaxWidth() int {
	if m.width <= 0 {
		return 60
	}
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

func saveConfigCmd(cfg state.Config) tea.Cmd {
	return func() tea.Msg {
		_ = state.SaveConfig(cfg)
		return nil
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
