package query

// Schema defines the database schema for issues, runs, and events
const Schema = `
-- Issues table
CREATE TABLE issues (
    id TEXT PRIMARY KEY,
    title TEXT,
    topic TEXT,
    summary TEXT,
    status TEXT,
    body TEXT,
    path TEXT
);

-- Issue tags junction table (for tag-based queries)
CREATE TABLE issue_tags (
    issue_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    PRIMARY KEY (issue_id, tag),
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);

-- Runs table
CREATE TABLE runs (
    issue_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    hex_id TEXT,
    status TEXT,
    phase TEXT,
    agent TEXT,
    model TEXT,
    model_variant TEXT,
    branch TEXT,
    worktree_path TEXT,
    session_name TEXT,
    agent_session_id TEXT,
    agent_session_generation INTEGER,
    pr_url TEXT,
    started_at TEXT,
    updated_at TEXT,
    continued_from TEXT,
    PRIMARY KEY (issue_id, run_id),
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);

-- Events table (opt-in via --with-events)
CREATE TABLE events (
    issue_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    attrs TEXT,
    raw TEXT,
    FOREIGN KEY (issue_id, run_id) REFERENCES runs(issue_id, run_id)
);

CREATE INDEX idx_events_run ON events(issue_id, run_id);
CREATE INDEX idx_events_type ON events(type);
CREATE INDEX idx_runs_status ON runs(status);
CREATE INDEX idx_runs_issue ON runs(issue_id);
CREATE INDEX idx_issue_tags_tag ON issue_tags(tag);
`

// Views defines convenience views with computed columns
const Views = `
-- Issues view with computed columns
CREATE VIEW issues_v AS
SELECT
    i.id,
    i.title,
    i.topic,
    i.summary,
    i.status,
    i.path,
    (SELECT COUNT(*) FROM runs r WHERE r.issue_id = i.id) AS run_count,
    (SELECT GROUP_CONCAT(tag, ', ') FROM issue_tags t WHERE t.issue_id = i.id) AS tags
FROM issues i;

-- Runs view with computed columns and issue info
CREATE VIEW runs_v AS
SELECT
    r.issue_id,
    r.run_id,
    r.hex_id,
    r.status,
    r.phase,
    r.agent,
    r.model,
    r.model_variant,
    r.branch,
    r.worktree_path,
    r.session_name,
    r.agent_session_id,
    r.agent_session_generation,
    r.pr_url,
    r.started_at,
    r.updated_at,
    r.continued_from,
    i.title AS issue_title,
    i.status AS issue_status,
    (SELECT COUNT(*) FROM events e WHERE e.issue_id = r.issue_id AND e.run_id = r.run_id) AS event_count
FROM runs r
LEFT JOIN issues i ON i.id = r.issue_id;
`

// CreateSchema creates the database schema
func CreateSchema(db *DB) error {
	if err := db.exec(Schema); err != nil {
		return err
	}
	return nil
}

// CreateViews creates the convenience views
func CreateViews(db *DB) error {
	if err := db.exec(Views); err != nil {
		return err
	}
	return nil
}
