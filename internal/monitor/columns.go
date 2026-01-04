package monitor

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
)

type ColumnID string

const (
	ColIndex       ColumnID = "index"
	ColID          ColumnID = "id"
	ColIssue       ColumnID = "issue"
	ColIssueStatus ColumnID = "issue_status"
	ColAgent       ColumnID = "agent"
	ColStatus      ColumnID = "status"
	ColAlive       ColumnID = "alive"
	ColBranch      ColumnID = "branch"
	ColWorktree    ColumnID = "worktree"
	ColPR          ColumnID = "pr"
	ColMerged      ColumnID = "merged"
	ColStarted     ColumnID = "started"
	ColUpdated     ColumnID = "updated"
	ColTopic       ColumnID = "topic"
)

type ColumnDef struct {
	ID       ColumnID
	Header   string
	Width    int
	Flexible bool
}

var columnRegistry = map[ColumnID]ColumnDef{
	ColIndex:       {ID: ColIndex, Header: "#", Width: 2},
	ColID:          {ID: ColID, Header: "ID", Width: 6},
	ColIssue:       {ID: ColIssue, Header: "ISSUE", Width: 14},
	ColIssueStatus: {ID: ColIssueStatus, Header: "ISSUE-ST", Width: 8},
	ColAgent:       {ID: ColAgent, Header: "AGENT", Width: agent.MaxAgentDisplayWidth},
	ColStatus:      {ID: ColStatus, Header: "STATUS", Width: 10},
	ColAlive:       {ID: ColAlive, Header: "ALIVE", Width: 5},
	ColBranch:      {ID: ColBranch, Header: "BRANCH", Width: runTableBranchWidth},
	ColWorktree:    {ID: ColWorktree, Header: "WORKTREE", Width: runTableWorktreeWidth},
	ColPR:          {ID: ColPR, Header: "PR", Width: 6},
	ColMerged:      {ID: ColMerged, Header: "MERGED", Width: 8},
	ColStarted:     {ID: ColStarted, Header: "STARTED", Width: 7},
	ColUpdated:     {ID: ColUpdated, Header: "UPDATED", Width: 7},
	ColTopic:       {ID: ColTopic, Header: "TOPIC", Width: 6, Flexible: true},
}

var defaultColumns = []ColumnID{
	ColIndex,
	ColID,
	ColIssue,
	ColIssueStatus,
	ColAgent,
	ColStatus,
	ColAlive,
	ColBranch,
	ColWorktree,
	ColPR,
	ColMerged,
	ColStarted,
	ColUpdated,
	ColTopic,
}

func GetColumnDef(id ColumnID) (ColumnDef, bool) {
	def, ok := columnRegistry[id]
	return def, ok
}

func DefaultColumns() []ColumnID {
	return defaultColumns
}

func LoadColumns(cfg *config.Config) []ColumnID {
	if cfg == nil || len(cfg.Monitor.PSColumns) == 0 {
		return defaultColumns
	}
	cols := make([]ColumnID, 0, len(cfg.Monitor.PSColumns))
	for _, name := range cfg.Monitor.PSColumns {
		id := ColumnID(name)
		if _, ok := columnRegistry[id]; ok {
			cols = append(cols, id)
		}
	}
	if len(cols) == 0 {
		return defaultColumns
	}
	return cols
}

func GetColumnValue(col ColumnID, row *RunRow, now time.Time) string {
	if row == nil {
		return "-"
	}
	switch col {
	case ColIndex:
		return fmt.Sprintf("%d", row.Index)
	case ColID:
		return row.ShortID
	case ColIssue:
		return row.IssueID
	case ColIssueStatus:
		return row.IssueStatus
	case ColAgent:
		return row.Agent
	case ColStatus:
		return string(row.Status)
	case ColAlive:
		return row.Alive
	case ColBranch:
		return row.Branch
	case ColWorktree:
		return row.Worktree
	case ColPR:
		return row.PR
	case ColMerged:
		return row.Merged
	case ColStarted:
		return formatRelativeTime(row.Started, now)
	case ColUpdated:
		return formatRelativeTime(row.Updated, now)
	case ColTopic:
		return row.Topic
	default:
		return "-"
	}
}

func GetColumnStyle(col ColumnID, row *RunRow, styles *Styles, isHeader bool) lipgloss.Style {
	if isHeader {
		return styles.Header
	}
	if row == nil {
		return styles.Text
	}
	switch col {
	case ColStatus:
		if style, ok := styles.Status[row.Status]; ok {
			return style
		}
	case ColAlive:
		if style, ok := styles.Alive[row.Alive]; ok {
			return style
		}
	case ColPR:
		if row.PRState != "" {
			if style, ok := styles.PRState[row.PRState]; ok {
				return style
			}
		}
	}
	return styles.Text
}
