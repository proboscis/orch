package monitor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s22625/orch/internal/model"
)

type runFilterState struct {
	RunFilter
	cursor int
}

type filterRowKind int

const (
	filterRowHeader filterRowKind = iota
	filterRowStatus
	filterRowAgent
	filterRowMerged
	filterRowPR
	filterRowIssueStatus
	filterRowIssueQuery
	filterRowUpdatedWithin
	filterRowAction
)

const (
	filterActionApply  = "apply"
	filterActionClear  = "clear"
	filterActionCancel = "cancel"
)

type filterRow struct {
	kind       filterRowKind
	value      string
	label      string
	selectable bool
}

type runFilterPreset struct {
	name  string
	apply func() RunFilter
}

var runFilterPresets = []runFilterPreset{
	{
		name: "only running",
		apply: func() RunFilter {
			filter := DefaultRunFilter()
			filter.Statuses = map[model.Status]bool{
				model.StatusRunning: true,
			}
			return filter
		},
	},
	{
		name: "needs attention",
		apply: func() RunFilter {
			filter := DefaultRunFilter()
			filter.Statuses = map[model.Status]bool{
				model.StatusBlocked:    true,
				model.StatusBlockedAPI: true,
				model.StatusFailed:     true,
			}
			return filter
		},
	},
	{
		name: "active",
		apply: func() RunFilter {
			return DefaultRunFilter()
		},
	},
}

func (d *Dashboard) enterFilterMode() (tea.Model, tea.Cmd) {
	d.filter = runFilterState{
		RunFilter: d.monitor.RunFilter().Clone(),
		cursor:    0,
	}
	d.message = ""
	d.ensureFilterCursor()
	d.mode = modeDashboardFilter
	return d, nil
}

func (d *Dashboard) applyQuickFilterPreset() (tea.Model, tea.Cmd) {
	if len(runFilterPresets) == 0 {
		return d, nil
	}
	d.filterPreset = (d.filterPreset + 1) % len(runFilterPresets)
	preset := runFilterPresets[d.filterPreset]
	d.monitor.SetRunFilter(preset.apply())
	d.message = fmt.Sprintf("filter preset: %s", preset.name)
	d.refreshing = true
	return d, d.refreshCmd()
}

func (d *Dashboard) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeDashboard
		return d, nil
	case "q":
		return d.quit()
	case "up", "k":
		d.moveFilterCursor(-1)
		return d, nil
	case "down", "j":
		d.moveFilterCursor(1)
		return d, nil
	case "enter", " ":
		return d.activateFilterRow()
	case "a":
		return d.applyFilter()
	case "c":
		return d.clearFilter()
	}

	row, ok := d.currentFilterRow()
	if !ok {
		return d, nil
	}

	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		switch row.kind {
		case filterRowIssueQuery:
			d.filter.IssueQuery = trimLastRune(d.filter.IssueQuery)
			d.filter.IssueRegex = nil
		case filterRowUpdatedWithin:
			d.filter.UpdatedWithinRaw = trimLastRune(d.filter.UpdatedWithinRaw)
			d.filter.UpdatedWithin = 0
		}
	case tea.KeyRunes:
		switch row.kind {
		case filterRowIssueQuery:
			d.filter.IssueQuery += string(msg.Runes)
			d.filter.IssueRegex = nil
		case filterRowUpdatedWithin:
			d.filter.UpdatedWithinRaw += string(msg.Runes)
			d.filter.UpdatedWithin = 0
		}
	}

	return d, nil
}

func (d *Dashboard) activateFilterRow() (tea.Model, tea.Cmd) {
	row, ok := d.currentFilterRow()
	if !ok {
		return d, nil
	}

	switch row.kind {
	case filterRowStatus:
		if row.value == agentFilterAll {
			d.filter.Statuses = allStatusSet()
			return d, nil
		}
		status := model.Status(row.value)
		if d.filter.Statuses == nil {
			d.filter.Statuses = make(map[model.Status]bool)
		}
		if d.filter.Statuses[status] {
			delete(d.filter.Statuses, status)
		} else {
			d.filter.Statuses[status] = true
		}
	case filterRowAgent:
		d.filter.Agent = row.value
	case filterRowMerged:
		d.filter.Merged = row.value
	case filterRowPR:
		d.filter.PR = row.value
	case filterRowIssueStatus:
		d.filter.IssueStatus = row.value
	case filterRowAction:
		switch row.value {
		case filterActionApply:
			return d.applyFilter()
		case filterActionClear:
			return d.clearFilter()
		case filterActionCancel:
			d.mode = modeDashboard
			return d, nil
		}
	}
	return d, nil
}

func (d *Dashboard) applyFilter() (tea.Model, tea.Cmd) {
	filter := d.filter.RunFilter.Clone()
	filter.IssueQuery = strings.TrimSpace(filter.IssueQuery)
	filter.UpdatedWithinRaw = strings.TrimSpace(filter.UpdatedWithinRaw)
	filter.IssueRegex = nil
	if re, _, err := compileIssueQuery(filter.IssueQuery); err != nil {
		d.message = fmt.Sprintf("issue filter: %v", err)
		return d, nil
	} else {
		filter.IssueRegex = re
	}
	duration, err := parseFilterDuration(filter.UpdatedWithinRaw)
	if err != nil {
		d.message = err.Error()
		return d, nil
	}
	filter.UpdatedWithin = duration
	d.monitor.SetRunFilter(filter)
	d.message = ""
	d.mode = modeDashboard
	d.refreshing = true
	return d, d.refreshCmd()
}

func (d *Dashboard) clearFilter() (tea.Model, tea.Cmd) {
	d.monitor.SetRunFilter(DefaultRunFilter())
	d.message = ""
	d.mode = modeDashboard
	d.refreshing = true
	return d, d.refreshCmd()
}

func (d *Dashboard) viewFilter() string {
	header := d.styles.Title.Render("FILTER RUNS")
	lines := []string{header, ""}
	rows := d.filterRows()
	for i, row := range rows {
		line := row.label
		if row.selectable && i == d.filter.cursor {
			line = d.styles.Selected.Render(line)
		}
		lines = append(lines, truncate(line, d.safeWidth()-2))
	}
	if d.message != "" {
		lines = append(lines, "", d.styles.Faint.Render(d.message))
	}
	lines = append(lines, "", "[Enter/Space] toggle/select  [a] apply  [c] clear  [Esc] cancel")
	return strings.Join(lines, "\n")
}

func (d *Dashboard) filterRows() []filterRow {
	var rows []filterRow

	rows = append(rows, filterRow{label: "Status:", selectable: false})
	allSelected := equalStatusSet(d.filter.Statuses, allStatusSet())
	rows = append(rows, filterRow{
		kind:       filterRowStatus,
		value:      agentFilterAll,
		label:      fmt.Sprintf("  %s all", checkbox(allSelected)),
		selectable: true,
	})
	for _, status := range runStatusOptions {
		rows = append(rows, filterRow{
			kind:       filterRowStatus,
			value:      string(status),
			label:      fmt.Sprintf("  %s %s", checkbox(d.filter.Statuses[status]), status),
			selectable: true,
		})
	}

	rows = append(rows, filterRow{label: "", selectable: false})
	rows = append(rows, filterRow{label: "Agent:", selectable: false})
	agentOptions := append([]string{agentFilterAll}, runAgentOptions...)
	for _, agent := range agentOptions {
		rows = append(rows, filterRow{
			kind:       filterRowAgent,
			value:      agent,
			label:      fmt.Sprintf("  %s %s", checkbox(d.filter.Agent == agent), agent),
			selectable: true,
		})
	}

	rows = append(rows, filterRow{label: "", selectable: false})
	rows = append(rows, filterRow{label: "Merged:", selectable: false})
	mergedOptions := []string{mergedFilterAll, mergedFilterConflict, mergedFilterClean, mergedFilterMerged, mergedFilterNoChange}
	for _, option := range mergedOptions {
		rows = append(rows, filterRow{
			kind:       filterRowMerged,
			value:      option,
			label:      fmt.Sprintf("  %s %s", checkbox(d.filter.Merged == option), option),
			selectable: true,
		})
	}

	rows = append(rows, filterRow{label: "", selectable: false})
	rows = append(rows, filterRow{label: "PR:", selectable: false})
	prOptions := []struct {
		value string
		label string
	}{
		{prFilterAll, "all"},
		{prFilterHas, "has PR"},
		{prFilterNone, "no PR"},
	}
	for _, option := range prOptions {
		rows = append(rows, filterRow{
			kind:       filterRowPR,
			value:      option.value,
			label:      fmt.Sprintf("  %s %s", checkbox(d.filter.PR == option.value), option.label),
			selectable: true,
		})
	}

	rows = append(rows, filterRow{label: "", selectable: false})
	rows = append(rows, filterRow{label: "Issue status:", selectable: false})
	for _, option := range runIssueStatusOptions {
		rows = append(rows, filterRow{
			kind:       filterRowIssueStatus,
			value:      option,
			label:      fmt.Sprintf("  %s %s", checkbox(d.filter.IssueStatus == option), option),
			selectable: true,
		})
	}

	rows = append(rows, filterRow{label: "", selectable: false})
	rows = append(rows, filterRow{
		kind:       filterRowIssueQuery,
		label:      fmt.Sprintf("Issue ID: %s (partial or /regex/)", formatInput(d.filter.IssueQuery)),
		selectable: true,
	})
	rows = append(rows, filterRow{
		kind:       filterRowUpdatedWithin,
		label:      fmt.Sprintf("Updated within: %s (e.g. 24h, 7d)", formatInput(d.filter.UpdatedWithinRaw)),
		selectable: true,
	})

	rows = append(rows, filterRow{label: "", selectable: false})
	rows = append(rows, filterRow{
		kind:       filterRowAction,
		value:      filterActionApply,
		label:      "[Apply]",
		selectable: true,
	})
	rows = append(rows, filterRow{
		kind:       filterRowAction,
		value:      filterActionClear,
		label:      "[Clear]",
		selectable: true,
	})
	rows = append(rows, filterRow{
		kind:       filterRowAction,
		value:      filterActionCancel,
		label:      "[Cancel]",
		selectable: true,
	})

	return rows
}

func (d *Dashboard) currentFilterRow() (filterRow, bool) {
	rows := d.filterRows()
	if len(rows) == 0 {
		return filterRow{}, false
	}
	if d.filter.cursor < 0 || d.filter.cursor >= len(rows) {
		return filterRow{}, false
	}
	return rows[d.filter.cursor], true
}

func (d *Dashboard) moveFilterCursor(delta int) {
	rows := d.filterRows()
	if len(rows) == 0 {
		d.filter.cursor = 0
		return
	}
	cursor := d.filter.cursor
	for i := 0; i < len(rows); i++ {
		cursor += delta
		if cursor < 0 {
			cursor = len(rows) - 1
		} else if cursor >= len(rows) {
			cursor = 0
		}
		if rows[cursor].selectable {
			d.filter.cursor = cursor
			return
		}
	}
}

func (d *Dashboard) ensureFilterCursor() {
	rows := d.filterRows()
	if len(rows) == 0 {
		d.filter.cursor = 0
		return
	}
	if d.filter.cursor >= 0 && d.filter.cursor < len(rows) && rows[d.filter.cursor].selectable {
		return
	}
	for i, row := range rows {
		if row.selectable {
			d.filter.cursor = i
			return
		}
	}
	d.filter.cursor = 0
}

func checkbox(checked bool) string {
	if checked {
		return "[x]"
	}
	return "[ ]"
}

func formatInput(value string) string {
	if strings.TrimSpace(value) == "" {
		return "[ ]"
	}
	return "[" + value + "]"
}

func trimLastRune(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	return string(runes[:len(runes)-1])
}
