package monitor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/s22625/orch/internal/model"
)

// SortKey defines the supported sort keys for monitor panes.
type SortKey string

const (
	SortByName    SortKey = "name"
	SortByUpdated SortKey = "updated"
	SortByStatus  SortKey = "status"
)

var sortKeyCycle = []SortKey{SortByName, SortByUpdated, SortByStatus}

// ParseSortKey validates a sort key string, applying a fallback when empty.
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
	default:
		return "", fmt.Errorf("invalid sort key %q (valid: %s)", value, strings.Join(ValidSortKeys(), ", "))
	}
}

// ValidSortKeys returns the supported sort key strings.
func ValidSortKeys() []string {
	return []string{string(SortByName), string(SortByUpdated), string(SortByStatus)}
}

// IsValidSortKey returns true when the key is recognized.
func IsValidSortKey(key SortKey) bool {
	switch key {
	case SortByName, SortByUpdated, SortByStatus:
		return true
	default:
		return false
	}
}

// NextSortKey cycles to the next sort key.
func NextSortKey(current SortKey) SortKey {
	for i, key := range sortKeyCycle {
		if key == current {
			return sortKeyCycle[(i+1)%len(sortKeyCycle)]
		}
	}
	return sortKeyCycle[0]
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
	if len(rows) < 2 {
		if len(rows) == 1 {
			rows[0].Index = 1
		}
		return
	}

	if !IsValidSortKey(key) {
		key = SortByUpdated
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a := rows[i]
		b := rows[j]

		switch key {
		case SortByName:
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			if cmp := strings.Compare(runRowRunID(a), runRowRunID(b)); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID < b.ShortID
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
			if cmp := strings.Compare(runRowRunID(a), runRowRunID(b)); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID < b.ShortID
		case SortByUpdated:
			fallthrough
		default:
			if !a.Updated.Equal(b.Updated) {
				return a.Updated.After(b.Updated)
			}
			if cmp := strings.Compare(a.IssueID, b.IssueID); cmp != 0 {
				return cmp < 0
			}
			if cmp := strings.Compare(runRowRunID(a), runRowRunID(b)); cmp != 0 {
				return cmp < 0
			}
			return a.ShortID < b.ShortID
		}
	})

	for i := range rows {
		rows[i].Index = i + 1
	}
}

func sortIssueRows(rows []IssueRow, key SortKey) {
	if len(rows) < 2 {
		if len(rows) == 1 {
			rows[0].Index = 1
		}
		return
	}

	if !IsValidSortKey(key) {
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
				return a.LatestUpdated.After(b.LatestUpdated)
			}
			return a.ID < b.ID
		case SortByName:
			fallthrough
		default:
			if cmp := strings.Compare(a.ID, b.ID); cmp != 0 {
				return cmp < 0
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
