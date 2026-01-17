package monitor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
)

// SortKey defines the supported sort keys for monitor panes.
type SortKey string

// SortDirection indicates ascending or descending sort order.
type SortDirection int

const (
	SortAsc  SortDirection = 0
	SortDesc SortDirection = 1
)

// Run sort keys
const (
	SortByName    SortKey = "name"
	SortByUpdated SortKey = "updated"
	SortByStatus  SortKey = "status"
	SortByStarted SortKey = "started"
	SortByIssue   SortKey = "issue"
	SortByAgent   SortKey = "agent"
	SortByElapsed SortKey = "elapsed"
)

// Issue-specific sort keys
const (
	SortByID       SortKey = "id"       // alias for name
	SortByTitle    SortKey = "title"    // issue title
	SortByPriority SortKey = "priority" // issue priority
)

// runSortKeyCycle defines the cycle order for run sorting
var runSortKeyCycle = []SortKey{SortByUpdated, SortByStarted, SortByStatus, SortByIssue, SortByAgent, SortByElapsed}

// issueSortKeyCycle defines the cycle order for issue sorting
var issueSortKeyCycle = []SortKey{SortByName, SortByStatus, SortByTitle, SortByPriority, SortByUpdated}

// Legacy cycle for backward compatibility
var sortKeyCycle = []SortKey{SortByName, SortByUpdated, SortByStatus}

func ParseSortKey(value string, fallback SortKey) (SortKey, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		if IsValidSortKey(fallback) {
			return fallback, nil
		}
		return SortByUpdated, nil
	}
	switch trimmed {
	case string(SortByName), "id":
		return SortByName, nil
	case string(SortByUpdated):
		return SortByUpdated, nil
	case string(SortByStatus):
		return SortByStatus, nil
	case string(SortByStarted):
		return SortByStarted, nil
	case string(SortByIssue):
		return SortByIssue, nil
	case string(SortByAgent):
		return SortByAgent, nil
	case string(SortByElapsed):
		return SortByElapsed, nil
	case string(SortByTitle):
		return SortByTitle, nil
	case string(SortByPriority):
		return SortByPriority, nil
	default:
		return "", fmt.Errorf("invalid sort key %q (valid: %s)", value, strings.Join(ValidSortKeys(), ", "))
	}
}

func ValidSortKeys() []string {
	return []string{
		string(SortByName), string(SortByUpdated), string(SortByStatus),
		string(SortByStarted), string(SortByIssue), string(SortByAgent), string(SortByElapsed),
		string(SortByTitle), string(SortByPriority),
	}
}

func IsValidSortKey(key SortKey) bool {
	switch key {
	case SortByName, SortByUpdated, SortByStatus, SortByStarted, SortByIssue, SortByAgent, SortByElapsed, SortByTitle, SortByPriority:
		return true
	default:
		return false
	}
}

func IsValidRunSortKey(key SortKey) bool {
	switch key {
	case SortByUpdated, SortByStarted, SortByStatus, SortByIssue, SortByAgent, SortByElapsed, SortByName:
		return true
	default:
		return false
	}
}

func IsValidIssueSortKey(key SortKey) bool {
	switch key {
	case SortByName, SortByID, SortByStatus, SortByTitle, SortByPriority, SortByUpdated:
		return true
	default:
		return false
	}
}

func NextSortKey(current SortKey) SortKey {
	for i, key := range sortKeyCycle {
		if key == current {
			return sortKeyCycle[(i+1)%len(sortKeyCycle)]
		}
	}
	return sortKeyCycle[0]
}

func NextRunSortKey(current SortKey) SortKey {
	for i, key := range runSortKeyCycle {
		if key == current {
			return runSortKeyCycle[(i+1)%len(runSortKeyCycle)]
		}
	}
	return runSortKeyCycle[0]
}

func NextIssueSortKey(current SortKey) SortKey {
	for i, key := range issueSortKeyCycle {
		if key == current {
			return issueSortKeyCycle[(i+1)%len(issueSortKeyCycle)]
		}
	}
	return issueSortKeyCycle[0]
}

func DefaultSortDirection(key SortKey) SortDirection {
	switch key {
	case SortByUpdated, SortByStarted, SortByElapsed, SortByPriority:
		return SortDesc
	default:
		return SortAsc
	}
}

func SortIndicator(dir SortDirection) string {
	if dir == SortDesc {
		return "▼"
	}
	return "▲"
}

var runStatusOrder = map[model.Status]int{
	model.StatusRunning:    0,
	model.StatusBlocked:    1,
	model.StatusBlockedAPI: 2,
	model.StatusBooting:    3,
	model.StatusQueued:     4,
	model.StatusPROpen:     5,
	model.StatusDone:       6,
	model.StatusFailed:     7,
	model.StatusCanceled:   8,
	model.StatusUnknown:    9,
}

var issueStatusOrder = map[model.IssueStatus]int{
	model.IssueStatusOpen:     0,
	model.IssueStatusResolved: 1,
	model.IssueStatusClosed:   2,
}

func runStatusRank(status model.Status) int {
	if rank, ok := runStatusOrder[status]; ok {
		return rank
	}
	return len(runStatusOrder) + 1
}

func issueStatusRank(status model.IssueStatus) int {
	if rank, ok := issueStatusOrder[status]; ok {
		return rank
	}
	return len(issueStatusOrder) + 1
}

func runRowRunID(row RunRow) string {
	if row.Run == nil {
		return ""
	}
	return row.Run.RunID
}

func sortRunRows(rows []RunRow, key SortKey) {
	sortRunRowsWithDirection(rows, key, DefaultSortDirection(key))
}

func sortRunRowsWithDirection(rows []RunRow, key SortKey, dir SortDirection) {
	if len(rows) < 2 {
		if len(rows) == 1 {
			rows[0].Index = 1
		}
		return
	}

	if !IsValidRunSortKey(key) {
		key = SortByUpdated
	}

	now := time.Now()

	sort.SliceStable(rows, func(i, j int) bool {
		a := rows[i]
		b := rows[j]

		switch key {
		case SortByName:
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return (cmp < 0) == (dir == SortAsc)
			}
			if cmp := strings.Compare(runRowRunID(a), runRowRunID(b)); cmp != 0 {
				return (cmp < 0) == (dir == SortAsc)
			}
			return (a.ShortID < b.ShortID) == (dir == SortAsc)

		case SortByIssue:
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return (cmp < 0) == (dir == SortAsc)
			}
			if !a.Updated.Equal(b.Updated) {
				return a.Updated.After(b.Updated)
			}
			return (a.ShortID < b.ShortID) == (dir == SortAsc)

		case SortByAgent:
			if cmp := strings.Compare(a.Agent, b.Agent); cmp != 0 {
				return (cmp < 0) == (dir == SortAsc)
			}
			if !a.Updated.Equal(b.Updated) {
				return a.Updated.After(b.Updated)
			}
			return (a.ShortID < b.ShortID) == (dir == SortAsc)

		case SortByStatus:
			if ar, br := runStatusRank(a.Status), runStatusRank(b.Status); ar != br {
				return ar < br
			}
			if !a.Updated.Equal(b.Updated) {
				return a.Updated.After(b.Updated)
			}
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID < b.ShortID

		case SortByStarted:
			if !a.Started.Equal(b.Started) {
				return a.Started.After(b.Started) == (dir == SortDesc)
			}
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID < b.ShortID

		case SortByElapsed:
			aElapsed := now.Sub(a.Started)
			bElapsed := now.Sub(b.Started)
			if aElapsed != bElapsed {
				return (aElapsed > bElapsed) == (dir == SortDesc)
			}
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID < b.ShortID

		case SortByUpdated:
			fallthrough
		default:
			if !a.Updated.Equal(b.Updated) {
				return a.Updated.After(b.Updated) == (dir == SortDesc)
			}
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID < b.ShortID
		}
	})

	for i := range rows {
		rows[i].Index = i + 1
	}
}

var priorityOrder = map[string]int{
	"critical": 0,
	"high":     1,
	"medium":   2,
	"low":      3,
	"":         4,
}

func issuePriorityRank(priority string) int {
	priority = strings.ToLower(strings.TrimSpace(priority))
	if rank, ok := priorityOrder[priority]; ok {
		return rank
	}
	return len(priorityOrder)
}

func getIssuePriority(row *IssueRow) string {
	if row == nil || row.Issue == nil || row.Issue.Frontmatter == nil {
		return ""
	}
	return row.Issue.Frontmatter["priority"]
}

func getIssueTitle(row *IssueRow) string {
	if row == nil || row.Issue == nil {
		return row.Summary
	}
	if row.Issue.Title != "" {
		return row.Issue.Title
	}
	return row.Summary
}

func sortIssueRows(rows []IssueRow, key SortKey) {
	sortIssueRowsWithDirection(rows, key, DefaultSortDirection(key))
}

func sortIssueRowsWithDirection(rows []IssueRow, key SortKey, dir SortDirection) {
	if len(rows) < 2 {
		if len(rows) == 1 {
			rows[0].Index = 1
		}
		return
	}

	if !IsValidIssueSortKey(key) {
		key = SortByName
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a := rows[i]
		b := rows[j]

		switch key {
		case SortByStatus:
			aStatus := model.ParseIssueStatus(a.Status)
			bStatus := model.ParseIssueStatus(b.Status)
			if ar, br := issueStatusRank(aStatus), issueStatusRank(bStatus); ar != br {
				return ar < br
			}
			if a.LatestUpdated.IsZero() != b.LatestUpdated.IsZero() {
				return !a.LatestUpdated.IsZero()
			}
			if !a.LatestUpdated.Equal(b.LatestUpdated) {
				return a.LatestUpdated.After(b.LatestUpdated)
			}
			return a.ID < b.ID

		case SortByUpdated:
			if a.LatestUpdated.IsZero() != b.LatestUpdated.IsZero() {
				return !a.LatestUpdated.IsZero()
			}
			if !a.LatestUpdated.Equal(b.LatestUpdated) {
				return a.LatestUpdated.After(b.LatestUpdated) == (dir == SortDesc)
			}
			return a.ID < b.ID

		case SortByTitle:
			aTitle := strings.ToLower(getIssueTitle(&a))
			bTitle := strings.ToLower(getIssueTitle(&b))
			if cmp := strings.Compare(aTitle, bTitle); cmp != 0 {
				return (cmp < 0) == (dir == SortAsc)
			}
			return a.ID < b.ID

		case SortByPriority:
			aPriority := issuePriorityRank(getIssuePriority(&a))
			bPriority := issuePriorityRank(getIssuePriority(&b))
			if aPriority != bPriority {
				return (aPriority < bPriority) == (dir == SortDesc)
			}
			if a.LatestUpdated.IsZero() != b.LatestUpdated.IsZero() {
				return !a.LatestUpdated.IsZero()
			}
			if !a.LatestUpdated.Equal(b.LatestUpdated) {
				return a.LatestUpdated.After(b.LatestUpdated)
			}
			return a.ID < b.ID

		case SortByName, SortByID:
			fallthrough
		default:
			if cmp := strings.Compare(a.ID, b.ID); cmp != 0 {
				return (cmp < 0) == (dir == SortAsc)
			}
			if a.LatestUpdated.IsZero() != b.LatestUpdated.IsZero() {
				return !a.LatestUpdated.IsZero()
			}
			return a.LatestUpdated.After(b.LatestUpdated)
		}
	})

	for i := range rows {
		rows[i].Index = i + 1
	}
}

func sortRuns(runs []*model.Run, key SortKey) {
	if len(runs) < 2 {
		return
	}

	if !IsValidSortKey(key) {
		key = SortByUpdated
	}

	sort.SliceStable(runs, func(i, j int) bool {
		a := runs[i]
		b := runs[j]
		if a == nil || b == nil {
			return a != nil
		}

		switch key {
		case SortByName:
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			if cmp := strings.Compare(a.RunID, b.RunID); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID() < b.ShortID()
		case SortByStatus:
			if ar, br := runStatusRank(a.Status), runStatusRank(b.Status); ar != br {
				return ar < br
			}
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.After(b.UpdatedAt)
			}
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			if cmp := strings.Compare(a.RunID, b.RunID); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID() < b.ShortID()
		case SortByUpdated:
			fallthrough
		default:
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.After(b.UpdatedAt)
			}
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			if cmp := strings.Compare(a.RunID, b.RunID); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID() < b.ShortID()
		}
	})
}
