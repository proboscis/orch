package monitor

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
)

const (
	agentFilterAll       = "all"
	mergedFilterAll      = "all"
	mergedFilterClean    = "clean"
	mergedFilterConflict = "conflict"
	mergedFilterMerged   = "merged"
	mergedFilterNoChange = "no change"
	prFilterAll          = "all"
	prFilterHas          = "has"
	prFilterNone         = "none"
	issueStatusAll       = "all"
	issueStatusOpen      = "open"
	issueStatusResolved  = "resolved"
)

var runStatusOptions = []model.Status{
	model.StatusRunning,
	model.StatusBlocked,
	model.StatusBlockedAPI,
	model.StatusQueued,
	model.StatusBooting,
	model.StatusPROpen,
	model.StatusDone,
	model.StatusFailed,
	model.StatusCanceled,
	model.StatusUnknown,
}

var runAgentOptions = []string{
	"claude",
	"codex",
	"gemini",
	"custom",
}

var runIssueStatusOptions = []string{
	issueStatusAll,
	issueStatusOpen,
	issueStatusResolved,
}

// RunFilter holds the active filter settings for the runs dashboard.
type RunFilter struct {
	Statuses         map[model.Status]bool
	Agent            string
	IssueQuery       string
	IssueRegex       *regexp.Regexp
	Merged           string
	PR               string
	UpdatedWithin    time.Duration
	UpdatedWithinRaw string
	IssueStatus      string
}

func defaultStatusSet() map[model.Status]bool {
	set := make(map[model.Status]bool, len(runStatusOptions))
	for _, status := range defaultStatuses() {
		set[status] = true
	}
	return set
}

func allStatusSet() map[model.Status]bool {
	set := make(map[model.Status]bool, len(runStatusOptions))
	for _, status := range runStatusOptions {
		set[status] = true
	}
	return set
}

// DefaultRunFilter returns the baseline filter configuration.
func DefaultRunFilter() RunFilter {
	return RunFilter{
		Statuses:    defaultStatusSet(),
		Agent:       agentFilterAll,
		Merged:      mergedFilterAll,
		PR:          prFilterAll,
		IssueStatus: issueStatusAll,
	}
}

func newRunFilter(opts Options) RunFilter {
	filter := DefaultRunFilter()
	if len(opts.Statuses) > 0 {
		filter.Statuses = make(map[model.Status]bool, len(opts.Statuses))
		for _, status := range opts.Statuses {
			filter.Statuses[status] = true
		}
	}
	if strings.TrimSpace(opts.Issue) != "" {
		filter.IssueQuery = opts.Issue
	}
	return normalizeRunFilter(filter)
}

func normalizeRunFilter(filter RunFilter) RunFilter {
	if filter.Statuses == nil {
		filter.Statuses = defaultStatusSet()
	}
	filter.Agent = strings.ToLower(strings.TrimSpace(filter.Agent))
	if filter.Agent == "" {
		filter.Agent = agentFilterAll
	}
	filter.Merged = strings.ToLower(strings.TrimSpace(filter.Merged))
	if filter.Merged == "" {
		filter.Merged = mergedFilterAll
	}
	filter.PR = strings.ToLower(strings.TrimSpace(filter.PR))
	if filter.PR == "" {
		filter.PR = prFilterAll
	}
	filter.IssueStatus = strings.ToLower(strings.TrimSpace(filter.IssueStatus))
	if filter.IssueStatus == "" {
		filter.IssueStatus = issueStatusAll
	}
	filter.IssueQuery = strings.TrimSpace(filter.IssueQuery)
	filter.UpdatedWithinRaw = strings.TrimSpace(filter.UpdatedWithinRaw)
	return filter
}

// Clone copies the filter and its maps for safe editing.
func (f RunFilter) Clone() RunFilter {
	clone := f
	clone.Statuses = copyStatusSet(f.Statuses)
	return clone
}

// IsDefault returns true when the filter matches the default dashboard filter.
func (f RunFilter) IsDefault() bool {
	f = normalizeRunFilter(f)
	if !equalStatusSet(f.Statuses, defaultStatusSet()) {
		return false
	}
	if f.Agent != agentFilterAll {
		return false
	}
	if f.IssueQuery != "" {
		return false
	}
	if f.Merged != mergedFilterAll {
		return false
	}
	if f.PR != prFilterAll {
		return false
	}
	if f.IssueStatus != issueStatusAll {
		return false
	}
	if f.UpdatedWithin > 0 {
		return false
	}
	if f.UpdatedWithinRaw != "" {
		return false
	}
	return true
}

// Summary returns a human-readable summary of the filter state.
func (f RunFilter) Summary() string {
	f = normalizeRunFilter(f)
	var parts []string

	if statusLabel := f.statusSummary(); statusLabel != "" {
		parts = append(parts, statusLabel)
	}
	if f.Agent != agentFilterAll {
		parts = append(parts, fmt.Sprintf("agent=%s", f.Agent))
	}
	if f.Merged != mergedFilterAll {
		parts = append(parts, fmt.Sprintf("merged=%s", f.Merged))
	}
	if f.PR != prFilterAll {
		parts = append(parts, fmt.Sprintf("pr=%s", f.PR))
	}
	if f.IssueStatus != issueStatusAll {
		parts = append(parts, fmt.Sprintf("issue_status=%s", f.IssueStatus))
	}
	if f.IssueQuery != "" {
		parts = append(parts, fmt.Sprintf("issue=%s", f.IssueQuery))
	}
	if f.UpdatedWithin > 0 {
		label := f.UpdatedWithinRaw
		if label == "" {
			label = f.UpdatedWithin.String()
		}
		parts = append(parts, fmt.Sprintf("updated<=%s", label))
	}

	if len(parts) == 0 {
		return "filter: all"
	}
	return "filter: " + strings.Join(parts, " ")
}

func (f RunFilter) statusSummary() string {
	if len(f.Statuses) == 0 {
		return "status=none"
	}
	if equalStatusSet(f.Statuses, defaultStatusSet()) {
		return "status=active"
	}
	if equalStatusSet(f.Statuses, allStatusSet()) {
		return ""
	}
	var statuses []string
	for _, status := range runStatusOptions {
		if f.Statuses[status] {
			statuses = append(statuses, string(status))
		}
	}
	if len(statuses) == 0 {
		return "status=none"
	}
	return fmt.Sprintf("status=%s", strings.Join(statuses, ","))
}

func copyStatusSet(in map[model.Status]bool) map[model.Status]bool {
	if in == nil {
		return nil
	}
	out := make(map[model.Status]bool, len(in))
	for k, v := range in {
		if v {
			out[k] = true
		}
	}
	return out
}

func equalStatusSet(a, b map[model.Status]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func statusSlice(set map[model.Status]bool) []model.Status {
	if len(set) == 0 {
		return nil
	}
	slice := make([]model.Status, 0, len(set))
	for status := range set {
		slice = append(slice, status)
	}
	sort.Slice(slice, func(i, j int) bool {
		return runStatusRank(slice[i]) < runStatusRank(slice[j])
	})
	return slice
}

func compileIssueQuery(raw string) (*regexp.Regexp, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false, nil
	}
	if strings.HasPrefix(trimmed, "/") && strings.HasSuffix(trimmed, "/") && len(trimmed) > 2 {
		pattern := trimmed[1 : len(trimmed)-1]
		if strings.TrimSpace(pattern) == "" {
			return nil, true, fmt.Errorf("regex pattern is empty")
		}
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return nil, true, err
		}
		return re, true, nil
	}
	return nil, false, nil
}

func parseFilterDuration(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(trimmed); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return d, nil
	}
	re := regexp.MustCompile(`^(\d+)([dwDW])$`)
	matches := re.FindStringSubmatch(trimmed)
	if matches == nil {
		return 0, fmt.Errorf("invalid duration format: %s (use 24h, 7d, or 2w)", trimmed)
	}
	value, _ := strconv.Atoi(matches[1])
	unit := strings.ToLower(matches[2])
	switch unit {
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit: %s", unit)
	}
}

// FilterRows applies the filter to a list of run rows.
func (f RunFilter) FilterRows(rows []RunRow, now time.Time) []RunRow {
	f = normalizeRunFilter(f)
	if len(f.Statuses) == 0 {
		return nil
	}
	cutoff := time.Time{}
	if f.UpdatedWithin > 0 {
		cutoff = now.Add(-f.UpdatedWithin)
	}
	query := strings.ToLower(f.IssueQuery)

	var filtered []RunRow
	for _, row := range rows {
		if !f.Statuses[row.Status] {
			continue
		}
		if f.Agent != agentFilterAll {
			agent := ""
			if row.Run != nil {
				agent = row.Run.Agent
			}
			if agent == "" {
				agent = row.Agent
			}
			agent = strings.ToLower(strings.TrimSpace(agent))
			if agent != f.Agent {
				continue
			}
		}
		if f.IssueQuery != "" {
			if f.IssueRegex != nil {
				if !f.IssueRegex.MatchString(row.IssueID) {
					continue
				}
			} else if !strings.Contains(strings.ToLower(row.IssueID), query) {
				continue
			}
		}
		if f.Merged != mergedFilterAll {
			if strings.ToLower(row.Merged) != f.Merged {
				continue
			}
		}
		if f.PR != prFilterAll {
			hasPR := row.PR != "" && row.PR != "-"
			if f.PR == prFilterHas && !hasPR {
				continue
			}
			if f.PR == prFilterNone && hasPR {
				continue
			}
		}
		if f.IssueStatus != issueStatusAll {
			status := model.ParseIssueStatus(row.IssueStatus)
			if string(status) != f.IssueStatus {
				continue
			}
		}
		if !cutoff.IsZero() && row.Updated.Before(cutoff) {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func reindexRunRows(rows []RunRow) {
	for i := range rows {
		rows[i].Index = i + 1
	}
}
