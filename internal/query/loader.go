package query

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
)

// LoadIssues loads all issues into the database
func LoadIssues(db *DB, api orchapi.OrchAPI) error {
	ctx := context.Background()
	result, err := api.ListIssues(ctx, nil)
	if err != nil {
		return err
	}

	for _, issue := range result.Issues {
		modelIssue, err := apiIssueToModel(issue)
		if err != nil {
			return err
		}
		if err := insertIssue(db, modelIssue); err != nil {
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
func LoadRuns(db *DB, api orchapi.OrchAPI) error {
	ctx := context.Background()
	result, err := api.ListRuns(ctx, nil)
	if err != nil {
		return err
	}

	for _, run := range result.Runs {
		modelRun, err := apiRunToModel(run)
		if err != nil {
			return err
		}
		if err := insertRun(db, modelRun); err != nil {
			return err
		}
	}

	return nil
}

func insertRun(db *DB, run *model.Run) error {
	query := `INSERT INTO runs (issue_id, run_id, hex_id, status, phase, agent, model, model_variant,
		branch, worktree_path, session_name, pr_url, started_at, updated_at, continued_from)
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
		run.SessionName,
		run.PRUrl,
		startedAt,
		updatedAt,
		run.ContinuedFrom,
	)
}

// LoadEvents loads events for all runs into the database (opt-in)
func LoadEvents(db *DB, api orchapi.OrchAPI) error {
	ctx := context.Background()
	result, err := api.ListRuns(ctx, nil)
	if err != nil {
		return err
	}

	for _, run := range result.Runs {
		for _, event := range run.Events {
			modelEvent := &model.Event{
				Timestamp: event.Timestamp,
				Type:      model.EventType(event.Type),
				Name:      event.Name,
				Attrs:     event.Attrs,
			}
			if err := insertEvent(db, string(run.IssueID), string(run.RunID), modelEvent); err != nil {
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
func LoadAll(db *DB, api orchapi.OrchAPI, opts *LoadOptions) error {
	if err := LoadIssues(db, api); err != nil {
		return err
	}

	if err := LoadRuns(db, api); err != nil {
		return err
	}

	if opts != nil && opts.WithEvents {
		if err := LoadEvents(db, api); err != nil {
			return err
		}
	}

	return nil
}

// apiIssueToModel converts orchapi.Issue to model.Issue.
func apiIssueToModel(i *orchapi.Issue) (*model.Issue, error) {
	status, err := model.ParseIssueStatus(string(i.Status))
	if err != nil {
		return nil, fmt.Errorf("invalid issue status for %s: %w", i.ID, err)
	}
	return &model.Issue{
		ID:      i.ID,
		Title:   i.Title,
		Topic:   i.Topic,
		Summary: i.Summary,
		Status:  status,
		Tags:    i.Tags,
		Body:    i.Body,
		Path:    i.Path,
	}, nil
}

// apiRunToModel converts orchapi.Run to model.Run.
func apiRunToModel(r *orchapi.Run) (*model.Run, error) {
	status, err := model.NormalizeStatus(string(r.Status))
	if err != nil {
		return nil, fmt.Errorf("invalid run status for %s#%s: %w", r.IssueID, r.RunID, err)
	}
	run := &model.Run{
		IssueID:       r.IssueID,
		RunID:         r.RunID,
		Status:        status,
		Phase:         model.Phase(r.Phase),
		Agent:         r.Agent,
		Model:         r.Model,
		ModelVariant:  r.ModelVariant,
		Branch:        r.Branch,
		WorktreePath:  r.WorktreePath,
		SessionName:   r.SessionName,
		PRUrl:         r.PRUrl,
		ContinuedFrom: r.ContinuedFrom,
		StartedAt:     r.StartedAt,
		UpdatedAt:     r.UpdatedAt,
	}
	for _, e := range r.Events {
		run.Events = append(run.Events, &model.Event{
			Timestamp: e.Timestamp,
			Type:      model.EventType(e.Type),
			Name:      e.Name,
			Attrs:     e.Attrs,
		})
	}
	return run, nil
}
