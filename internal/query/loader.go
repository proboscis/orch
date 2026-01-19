package query

import (
	"encoding/json"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

// LoadIssues loads all issues into the database
func LoadIssues(db *DB, st store.Store) error {
	issues, err := st.ListIssues()
	if err != nil {
		return err
	}

	for _, issue := range issues {
		if err := insertIssue(db, issue); err != nil {
			return err
		}
	}

	return nil
}

func insertIssue(db *DB, issue *model.Issue) error {
	query := `INSERT INTO issues (id, title, topic, summary, status, body, path)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	if err := db.exec(query,
		issue.ID,
		issue.Title,
		issue.Topic,
		issue.Summary,
		string(issue.Status),
		issue.Body,
		issue.Path,
	); err != nil {
		return err
	}

	// Insert tags
	for _, tag := range issue.Tags {
		if err := db.exec(`INSERT INTO issue_tags (issue_id, tag) VALUES (?, ?)`,
			issue.ID, tag); err != nil {
			return err
		}
	}

	return nil
}

// LoadRuns loads all runs into the database
func LoadRuns(db *DB, st store.Store) error {
	runs, err := st.ListRuns(&store.ListRunsFilter{})
	if err != nil {
		return err
	}

	for _, run := range runs {
		if err := insertRun(db, run); err != nil {
			return err
		}
	}

	return nil
}

func insertRun(db *DB, run *model.Run) error {
	query := `INSERT INTO runs (issue_id, run_id, hex_id, status, phase, agent, model, model_variant,
		branch, worktree_path, tmux_session, pr_url, started_at, updated_at, continued_from)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var startedAt, updatedAt string
	if !run.StartedAt.IsZero() {
		startedAt = run.StartedAt.Format(time.RFC3339)
	}
	if !run.UpdatedAt.IsZero() {
		updatedAt = run.UpdatedAt.Format(time.RFC3339)
	}

	return db.exec(query,
		run.IssueID,
		run.RunID,
		run.ShortID(),
		string(run.Status),
		string(run.Phase),
		run.Agent,
		run.Model,
		run.ModelVariant,
		run.Branch,
		run.WorktreePath,
		run.TmuxSession,
		run.PRUrl,
		startedAt,
		updatedAt,
		run.ContinuedFrom,
	)
}

// LoadEvents loads events for all runs into the database (opt-in)
func LoadEvents(db *DB, st store.Store) error {
	runs, err := st.ListRuns(&store.ListRunsFilter{})
	if err != nil {
		return err
	}

	for _, run := range runs {
		for _, event := range run.Events {
			if err := insertEvent(db, run.IssueID, run.RunID, event); err != nil {
				return err
			}
		}
	}

	return nil
}

func insertEvent(db *DB, issueID, runID string, event *model.Event) error {
	query := `INSERT INTO events (issue_id, run_id, timestamp, type, name, attrs, raw)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	var attrsJSON string
	if len(event.Attrs) > 0 {
		if b, err := json.Marshal(event.Attrs); err == nil {
			attrsJSON = string(b)
		}
	}

	return db.exec(query,
		issueID,
		runID,
		event.Timestamp.Format(time.RFC3339),
		string(event.Type),
		event.Name,
		attrsJSON,
		event.Raw,
	)
}

// LoadOptions controls what data to load
type LoadOptions struct {
	WithEvents bool
}

// LoadAll loads issues, runs, and optionally events
func LoadAll(db *DB, st store.Store, opts *LoadOptions) error {
	if err := LoadIssues(db, st); err != nil {
		return err
	}

	if err := LoadRuns(db, st); err != nil {
		return err
	}

	if opts != nil && opts.WithEvents {
		if err := LoadEvents(db, st); err != nil {
			return err
		}
	}

	return nil
}
