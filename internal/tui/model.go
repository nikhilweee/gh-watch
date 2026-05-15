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
	modeSettings
)

type columnDef struct {
	id       string
	header   string
	sortable bool
}

var allColumns = []columnDef{
	{"PR", "PR", true},
	{"STATUS", "STATUS", true},
	{"AUTOMERGE", "AUTOMERGE", true},
	{"TITLE", "TITLE", true},
	{"AUTHOR", "AUTHOR", true},
	{"BASE", "BASE", true},
	{"REVIEWS", "REVIEWS", true},
	{"CHECKS", "CHECKS", true},
	{"UPDATED", "UPDATED", true},
}

var intervalOptions = []int{15, 30, 60, 120, 300, 900, 3600}

type settingsItem struct {
	kind  string // "interval" or "column"
	value int    // seconds, for interval items
	col   *columnDef
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

type pollAllDoneMsg []pollDoneMsg

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
	pickerCursor  int
	pollInterval  time.Duration
	settingsPanel int // 0 = interval, 1 = columns
	intervalCursor int
}

// columnsFromIDs builds a columnDef slice from IDs, preserving the given order.
// PR is always first.
func columnsFromIDs(ids []string) []columnDef {
	colsByID := make(map[string]columnDef, len(allColumns))
	for _, c := range allColumns {
		colsByID[c.id] = c
	}
	seen := map[string]bool{"PR": true}
	cols := []columnDef{colsByID["PR"]}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		c, ok := colsByID[id]
		if !ok {
			continue
		}
		cols = append(cols, c)
		seen[id] = true
	}
	return cols
}

// addColumn inserts col into cols at its natural allColumns position,
// preserving the relative order of already-visible columns.
func addColumn(cols []columnDef, col columnDef) []columnDef {
	colAllIdx := -1
	for i, c := range allColumns {
		if c.id == col.id {
			colAllIdx = i
			break
		}
	}
	ins := len(cols)
	for i, c := range cols {
		for j, ac := range allColumns {
			if ac.id == c.id {
				if j > colAllIdx {
					ins = i
				}
				break
			}
		}
		if ins != len(cols) {
			break
		}
	}
	result := make([]columnDef, 0, len(cols)+1)
	result = append(result, cols[:ins]...)
	result = append(result, col)
	result = append(result, cols[ins:]...)
	return result
}

// settingsColumnItems returns visible columns (in m.columns order) then hidden columns.
func settingsColumnItems(m Model) []settingsItem {
	items := make([]settingsItem, 0, len(allColumns))
	visibleSet := map[string]bool{}
	for _, c := range m.columns {
		visibleSet[c.id] = true
	}
	for i := range m.columns {
		for j := range allColumns {
			if allColumns[j].id == m.columns[i].id {
				items = append(items, settingsItem{kind: "column", col: &allColumns[j]})
				break
			}
		}
	}
	for i := range allColumns {
		if !visibleSet[allColumns[i].id] {
			items = append(items, settingsItem{kind: "column", col: &allColumns[i]})
		}
	}
	return items
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
		rows:         rows,
		spinner:      s,
		input:        ti,
		columns:      columnsFromIDs(cfg.Columns),
		sortBy:       cfg.SortBy,
		sortDesc:     cfg.SortDesc,
		pollInterval: time.Duration(cfg.PollInterval) * time.Second,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		tea.Tick(m.pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) }),
	}
	if len(m.rows) > 0 {
		watches := make([]state.Watch, len(m.rows))
		for i, r := range m.rows {
			watches[i] = r.watch
		}
		cmds = append(cmds, pollAllCmd(watches))
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
				return m, tea.Batch(addToState(w), pollAllCmd([]state.Watch{w}))
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.inputErr = ""
				return m, cmd
			}

		case modeSettings:
			colItems := settingsColumnItems(m)
			switch msg.Keystroke() {
			case "esc":
				m.mode = modeNormal
			case "left", "h":
				m.settingsPanel = 0
			case "right", "l":
				m.settingsPanel = 1
			case "up":
				if m.settingsPanel == 1 {
					if m.intervalCursor > 0 {
						m.intervalCursor--
					}
				} else {
					if m.pickerCursor > 0 {
						m.pickerCursor--
					}
				}
			case "down":
				if m.settingsPanel == 1 {
					if m.intervalCursor < len(intervalOptions)-1 {
						m.intervalCursor++
					}
				} else {
					if m.pickerCursor < len(colItems)-1 {
						m.pickerCursor++
					}
				}
			case "space":
				if m.settingsPanel == 1 {
					m.pollInterval = time.Duration(intervalOptions[m.intervalCursor]) * time.Second
					cfg := state.Config{Columns: columnIDs(m.columns), SortBy: m.sortBy, SortDesc: m.sortDesc, PollInterval: intervalOptions[m.intervalCursor]}
					return m, saveConfigCmd(cfg)
				}
				item := colItems[m.pickerCursor]
				if item.col.id == "PR" {
					break
				}
				found := false
				for i, c := range m.columns {
					if c.id == item.col.id {
						m.columns = append(m.columns[:i], m.columns[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					m.columns = append(m.columns, *item.col)
				}
				cfg := state.Config{Columns: columnIDs(m.columns), SortBy: m.sortBy, SortDesc: m.sortDesc, PollInterval: int(m.pollInterval.Seconds())}
				return m, saveConfigCmd(cfg)
			case "s":
				if m.settingsPanel != 0 {
					break
				}
				item := colItems[m.pickerCursor]
				if !item.col.sortable {
					break
				}
				id := item.col.id
				if id != m.sortBy {
					m.sortBy = id
					m.sortDesc = false
				} else if !m.sortDesc {
					m.sortDesc = true
				} else {
					m.sortBy = ""
					m.sortDesc = false
				}
				m.sortRows()
				cfg := state.Config{Columns: columnIDs(m.columns), SortBy: m.sortBy, SortDesc: m.sortDesc, PollInterval: int(m.pollInterval.Seconds())}
				return m, saveConfigCmd(cfg)
			case "k":
				if m.settingsPanel != 0 {
					break
				}
				item := colItems[m.pickerCursor]
				if item.col.id == "PR" {
					break
				}
				pos := -1
				for i, c := range m.columns {
					if c.id == item.col.id {
						pos = i
						break
					}
				}
				if pos <= 1 {
					break
				}
				m.columns[pos-1], m.columns[pos] = m.columns[pos], m.columns[pos-1]
				m.pickerCursor--
				cfg := state.Config{Columns: columnIDs(m.columns), SortBy: m.sortBy, SortDesc: m.sortDesc, PollInterval: int(m.pollInterval.Seconds())}
				return m, saveConfigCmd(cfg)
			case "j":
				if m.settingsPanel != 0 {
					break
				}
				item := colItems[m.pickerCursor]
				if item.col.id == "PR" {
					break
				}
				pos := -1
				for i, c := range m.columns {
					if c.id == item.col.id {
						pos = i
						break
					}
				}
				if pos < 0 || pos >= len(m.columns)-1 {
					break
				}
				m.columns[pos], m.columns[pos+1] = m.columns[pos+1], m.columns[pos]
				m.pickerCursor++
				cfg := state.Config{Columns: columnIDs(m.columns), SortBy: m.sortBy, SortDesc: m.sortDesc, PollInterval: int(m.pollInterval.Seconds())}
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
			case "s":
				m.mode = modeSettings
				m.settingsPanel = 0
				m.pickerCursor = 0
				for i, v := range intervalOptions {
					if time.Duration(v)*time.Second == m.pollInterval {
						m.intervalCursor = i
						break
					}
				}
				return m, nil
			case "r":
				var toRefresh []state.Watch
				for i := range m.rows {
					if m.rows[i].state != rowMerging {
						m.rows[i].state = rowPolling
						toRefresh = append(toRefresh, m.rows[i].watch)
					}
				}
				if len(toRefresh) > 0 {
					return m, pollAllCmd(toRefresh)
				}
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
			tea.Tick(m.pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) }),
		}
		var toPoll []state.Watch
		for i := range m.rows {
			if m.rows[i].state != rowMerging && m.rows[i].state != rowMerged && m.rows[i].state != rowClosed {
				m.rows[i].state = rowPolling
				toPoll = append(toPoll, m.rows[i].watch)
			}
		}
		if len(toPoll) > 0 {
			cmds = append(cmds, pollAllCmd(toPoll))
		}
		return m, tea.Batch(cmds...)

	case pollAllDoneMsg:
		var cmds []tea.Cmd
		now := time.Now()
		for _, msg := range msg {
			for i := range m.rows {
				w := m.rows[i].watch
				if w.PR != msg.pr || w.Repo != msg.repo {
					continue
				}
				m.rows[i].lastAt = now
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
						cmds = append(cmds, mergeCmd(w, msg.result.PR.NodeID))
					} else {
						m.rows[i].state = rowReady
					}
				}
				break
			}
		}
		m.sortRows()
		return m, tea.Batch(cmds...)

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
	case modeSettings:
		inner.WriteString(styleBold.Render("Settings") + "\n\n")

		// Left panel: Columns
		colItems := settingsColumnItems(m)
		activeSet := map[string]bool{}
		for _, c := range m.columns {
			activeSet[c.id] = true
		}
		var leftLines []string
		leftLines = append(leftLines, styleBold.Render("Columns"))
		for i, item := range colItems {
			cursor := "  "
			if m.settingsPanel == 0 && i == m.pickerCursor {
				cursor = styleGreen.Render("▶ ")
			}
			locked := item.col.id == "PR"
			var check, label string
			arrow := "  "
			if item.col.id == m.sortBy && m.sortBy != "" {
				if m.sortDesc {
					arrow = "↓ "
				} else {
					arrow = "↑ "
				}
			}
			if locked {
				check = styleMuted.Render("[x]")
				label = styleMuted.Render(arrow + item.col.header)
			} else if activeSet[item.col.id] {
				check = styleGreen.Render("[x]")
				if arrow != "  " {
					label = styleGreen.Render(arrow + item.col.header)
				} else {
					label = styleMuted.Render(arrow) + item.col.header
				}
			} else {
				check = styleMuted.Render("[ ]")
				label = styleMuted.Render(arrow + item.col.header)
			}
			leftLines = append(leftLines, fmt.Sprintf("%s%s  %s", cursor, check, label))
		}
		leftStr := lipgloss.NewStyle().Width(30).Render(strings.Join(leftLines, "\n"))

		// Right panel: Poll interval
		currentSecs := int(m.pollInterval.Seconds())
		var rightLines []string
		rightLines = append(rightLines, styleBold.Render("Poll Interval"))
		for i, v := range intervalOptions {
			cursor := "  "
			if m.settingsPanel == 1 && i == m.intervalCursor {
				cursor = styleGreen.Render("▶ ")
			}
			var check string
			if currentSecs == v {
				check = styleGreen.Render("[x]")
			} else {
				check = styleMuted.Render("[ ]")
			}
			var label string
			if v >= 3600 {
				label = fmt.Sprintf("%dh", v/3600)
			} else if v >= 60 {
				label = fmt.Sprintf("%dm", v/60)
			} else {
				label = fmt.Sprintf("%ds", v)
			}
			rightLines = append(rightLines, fmt.Sprintf("%s%s  %s", cursor, check, label))
		}
		rightStr := strings.Join(rightLines, "\n")

		inner.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftStr, rightStr))

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
						return s.Bold(true).Faint(true)
					}
					if row == m.cursor {
						s = s.Background(selectedBg)
					}
					if col >= 0 && col < len(m.columns) && m.columns[col].id == "PR" {
						s = s.Bold(true)
					}
					return s
				})
			varWidths := m.effectiveVarWidths()
			for i, row := range m.rows {
				cells := make([]string, len(m.columns))
				for j, col := range m.columns {
					cells[j] = m.cellValue(row, col, i == m.cursor, varWidths)
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
	case modeSettings:
		footer.WriteString(styleHelp.Render("[space] select/toggle  [s] sort  [j/k] reorder  [←/→] switch panel  [esc] close"))
	default:
		footer.WriteString(styleHelp.Render("[↑/↓] navigate  [a] add  [x] remove  [m] auto-merge  [o] open  [r] refresh  [s] settings  [q] quit"))
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

func (m Model) cellValue(row watchRow, col columnDef, selected bool, varWidths map[string]int) string {
	prevMerged := row.result != nil && row.result.PR.State == "MERGED"
	prevClosed := row.result != nil && row.result.PR.State == "CLOSED"
	dim := row.state == rowMerged || row.state == rowClosed ||
		(row.state == rowPolling && (prevMerged || prevClosed))
	r := row.result

	switch col.id {
	case "PR":
		s := fmt.Sprintf("%d", row.watch.PR)
		if row.state == rowPolling {
			spin := m.spinner.View()
			if pad := len(s) - lipgloss.Width(spin); pad > 0 {
				return spin + strings.Repeat(" ", pad)
			}
			return spin
		}
		if dim {
			return styleMuted.Render(s)
		}
		return s

	case "STATUS":
		if row.state == rowError {
			return styleRed.Render("error")
		}
		if r == nil {
			return ""
		}
		return mergeStatusLabel(r, selected)

	case "AUTOMERGE":
		if dim || !row.watch.AutoMerge {
			return ""
		}
		return styleYellow.Render("enabled")

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
		title := truncate(r.PR.Title, varWidths["TITLE"])
		if dim {
			return styleMuted.Render(title)
		}
		return title

	case "AUTHOR":
		if r == nil {
			return ""
		}
		author := truncate(r.PR.Author, varWidths["AUTHOR"])
		if dim {
			return styleMuted.Render(author)
		}
		return author

	case "BASE":
		if r == nil {
			return ""
		}
		base := truncate(r.PR.BaseRefName, varWidths["BASE"])
		if dim {
			return styleMuted.Render(base)
		}
		return base

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
		ri := m.rows[i].result
		rj := m.rows[j].result
		if ri == nil || rj == nil {
			return ri != nil
		}
		var less bool
		switch m.sortBy {
		case "PR":
			less = m.rows[i].watch.PR < m.rows[j].watch.PR
		case "STATUS":
			less = statusRank(ri) < statusRank(rj)
		case "AUTOMERGE":
			ai, aj := 0, 0
			if m.rows[i].watch.AutoMerge {
				ai = 1
			}
			if m.rows[j].watch.AutoMerge {
				aj = 1
			}
			less = ai < aj
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

func columnIDs(cols []columnDef) []string {
	ids := make([]string, len(cols))
	for i, c := range cols {
		ids[i] = c.id
	}
	return ids
}

const varColMinWidth = 10

// naturalAllWidths returns the max plain-text content width for every visible
// column by scanning all rows. Widths are content-only (no padding).
func (m Model) naturalAllWidths() map[string]int {
	widths := map[string]int{}
	for _, col := range m.columns {
		widths[col.id] = len(col.header)
	}

	statusWidths := map[string]int{
		"CLEAN": 5, "DIRTY": 8, "BLOCKED": 7, "BEHIND": 6, "UNSTABLE": 8, "DRAFT": 5,
	}

	for _, row := range m.rows {
		if _, ok := widths["PR"]; ok {
			if n := len(fmt.Sprintf("%d", row.watch.PR)); n > widths["PR"] {
				widths["PR"] = n
			}
		}
		r := row.result
		if r == nil {
			continue
		}
		if _, ok := widths["STATUS"]; ok {
			var labelW int
			switch r.PR.State {
			case "MERGED", "CLOSED":
				labelW = 6
			default:
				labelW = statusWidths[r.PR.MergeStateStatus]
			}
			if row.state == rowError {
				labelW = 5
			}
			if labelW > widths["STATUS"] {
				widths["STATUS"] = labelW
			}
		}
		if _, ok := widths["CHECKS"]; ok {
			s := fmt.Sprintf("%d/%d/%d", r.PassedChecks, r.RunningChecks, r.FailedChecks)
			if n := len(s); n > widths["CHECKS"] {
				widths["CHECKS"] = n
			}
		}
		if _, ok := widths["REVIEWS"]; ok {
			s := fmt.Sprintf("%d/%d/%d", r.ApprovedReviews, r.PendingReviews, r.ChangesReviews)
			if n := len(s); n > widths["REVIEWS"] {
				widths["REVIEWS"] = n
			}
		}
		if _, ok := widths["TITLE"]; ok {
			if n := len([]rune(r.PR.Title)); n > widths["TITLE"] {
				widths["TITLE"] = n
			}
		}
		if _, ok := widths["AUTHOR"]; ok {
			if n := len([]rune(r.PR.Author)); n > widths["AUTHOR"] {
				widths["AUTHOR"] = n
			}
		}
		if _, ok := widths["BASE"]; ok {
			if n := len([]rune(r.PR.BaseRefName)); n > widths["BASE"] {
				widths["BASE"] = n
			}
		}
	}
	return widths
}

// effectiveVarWidths returns the truncation limit for each visible variable column
// (TITLE, AUTHOR, BASE). If all content fits within the terminal, natural widths
// are returned unchanged. Otherwise space is distributed proportionally
// (TITLE 3x, AUTHOR 1x, BASE 1x) with a varColMinWidth floor, and surplus from
// columns that don't need their allocation is redistributed to those that do.
func (m Model) effectiveVarWidths() map[string]int {
	allNatural := m.naturalAllWidths()

	varIDs := map[string]bool{"TITLE": true, "AUTHOR": true, "BASE": true}
	weights := map[string]int{"TITLE": 3, "AUTHOR": 1, "BASE": 1}

	natural := map[string]int{}
	fixedTotal := 0
	for id, w := range allNatural {
		if varIDs[id] {
			natural[id] = w
		} else {
			fixedTotal += w + 2
		}
	}

	if len(natural) == 0 || m.width <= 0 {
		return natural
	}

	totalNatural := fixedTotal
	for _, w := range natural {
		totalNatural += w + 2
	}
	if totalNatural <= m.width {
		return natural // everything fits; no truncation needed
	}

	totalWeight := 0
	for id := range natural {
		totalWeight += weights[id]
	}

	budget := m.width - fixedTotal - (varColMinWidth+2)*len(natural)
	if budget < 0 {
		budget = 0
	}

	result := map[string]int{}
	for id := range natural {
		result[id] = varColMinWidth + budget*weights[id]/totalWeight
	}

	// Redistribute surplus from columns that don't need their full allocation.
	for range natural {
		freed := 0
		saturated := map[string]bool{}
		for id, nat := range natural {
			if nat < result[id] {
				freed += result[id] - nat
				result[id] = nat
				saturated[id] = true
			}
		}
		if freed == 0 {
			break
		}
		needWeight := 0
		for id := range natural {
			if !saturated[id] && natural[id] > result[id] {
				needWeight += weights[id]
			}
		}
		if needWeight == 0 {
			break
		}
		for id := range natural {
			if !saturated[id] && natural[id] > result[id] {
				result[id] += freed * weights[id] / needWeight
			}
		}
	}

	// Assign any leftover space (from integer division) in weight order.
	totalUsed := fixedTotal
	for _, w := range result {
		totalUsed += w + 2
	}
	leftover := m.width - totalUsed
	for _, id := range []string{"TITLE", "AUTHOR", "BASE"} {
		if leftover <= 0 {
			break
		}
		if _, ok := result[id]; !ok {
			continue
		}
		add := natural[id] - result[id]
		if add <= 0 {
			continue
		}
		if add > leftover {
			add = leftover
		}
		result[id] += add
		leftover -= add
	}

	return result
}

func mergeStatusLabel(r *gh.PollResult, selected bool) string {
	colorWord := func(style lipgloss.Style, word string) string {
		if selected {
			style = style.Background(selectedBg)
		}
		return style.Render(word)
	}
	switch r.PR.State {
	case "MERGED":
		return colorWord(styleMuted, "merged")
	case "CLOSED":
		return colorWord(styleMuted, "closed")
	}
	switch r.PR.MergeStateStatus {
	case "CLEAN":
		return colorWord(styleGreen, "ready")
	case "DIRTY":
		return colorWord(styleRed, "conflict")
	case "BLOCKED":
		return colorWord(styleRed, "blocked")
	case "BEHIND":
		return colorWord(styleYellow, "behind")
	case "UNSTABLE":
		return colorWord(styleYellow, "unstable")
	case "DRAFT":
		return colorWord(styleMuted, "draft")
	default:
		return ""
	}
}

func statusRank(r *gh.PollResult) int {
	switch r.PR.MergeStateStatus {
	case "DIRTY", "BLOCKED":
		return 0
	case "BEHIND", "UNSTABLE":
		return 1
	case "CLEAN":
		return 2
	default:
		return 3
	}
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
		return styleMuted.Render("/")
	}
	return styleMuted.Background(selectedBg).Render("/")
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

func pollAllCmd(watches []state.Watch) tea.Cmd {
	return func() tea.Msg {
		targets := make([]gh.WatchTarget, len(watches))
		for i, w := range watches {
			targets[i] = gh.WatchTarget{Repo: w.Repo, PR: w.PR}
		}
		results, errs := gh.GetAllPRStatuses(targets)
		msgs := make(pollAllDoneMsg, len(watches))
		for i, w := range watches {
			msgs[i] = pollDoneMsg{repo: w.Repo, pr: w.PR, result: results[i], err: errs[i]}
		}
		return msgs
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
