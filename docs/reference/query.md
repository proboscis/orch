# SQL Query Reference

orch supports querying issues and runs using SQL with an in-memory SQLite database. This enables powerful analysis and custom reporting.

## Quick Start

```bash
# List open issues
orch query "SELECT * FROM issues WHERE status = 'open'"

# Alias: orch q
orch q "SELECT id, title FROM issues"

# Output formats
orch q "SELECT * FROM runs" --format json
orch q "SELECT * FROM runs" --format tsv
```

## Database Schema

### Tables

#### `issues`

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT | Issue ID (primary key) |
| title | TEXT | Issue title |
| topic | TEXT | Issue topic |
| summary | TEXT | Short summary |
| status | TEXT | Issue status (open, resolved, closed) |
| body | TEXT | Issue body (markdown) |
| path | TEXT | File path |

#### `runs`

| Column | Type | Description |
|--------|------|-------------|
| issue_id | TEXT | Parent issue ID |
| run_id | TEXT | Run ID (timestamp) |
| hex_id | TEXT | Short hex ID (6 chars) |
| status | TEXT | Run status |
| phase | TEXT | Current phase |
| agent | TEXT | Agent type |
| model | TEXT | Model name |
| model_variant | TEXT | Model variant |
| branch | TEXT | Git branch |
| worktree_path | TEXT | Worktree path |
| session_name | TEXT | Multiplexer session name |
| pr_url | TEXT | Pull request URL |
| started_at | TEXT | First event timestamp |
| updated_at | TEXT | Last event timestamp |
| continued_from | TEXT | Run this run was restarted from |

#### `issue_tags`

| Column | Type | Description |
|--------|------|-------------|
| issue_id | TEXT | Issue ID |
| tag | TEXT | Tag value |

#### `events` (with `--with-events`)

| Column | Type | Description |
|--------|------|-------------|
| issue_id | TEXT | Parent issue ID |
| run_id | TEXT | Parent run ID |
| timestamp | TEXT | Event timestamp |
| type | TEXT | Event type |
| name | TEXT | Event name |
| attrs | TEXT | Event attributes (JSON) |
| raw | TEXT | Original event line |

### Views

#### `issues_v`

Extended issues view with computed columns:

| Column | Description |
|--------|-------------|
| (all from issues) | Base issue columns |
| run_count | Number of runs for this issue |
| tags | Comma-separated tags |

#### `runs_v`

Extended runs view with issue info:

| Column | Description |
|--------|-------------|
| (all from runs) | Base run columns |
| issue_title | Parent issue title |
| issue_status | Parent issue status |
| event_count | Number of events |

## Common Queries

### Issue Queries

```sql
-- All open issues
SELECT id, title, status 
FROM issues 
WHERE status = 'open';

-- Issues with specific tag
SELECT i.* 
FROM issues i
JOIN issue_tags t ON i.id = t.issue_id
WHERE t.tag = 'bug';

-- Issues without runs
SELECT i.* 
FROM issues i
LEFT JOIN runs r ON i.id = r.issue_id
WHERE r.run_id IS NULL;

-- Issues with active runs
SELECT DISTINCT i.* 
FROM issues i
JOIN runs r ON i.id = r.issue_id
WHERE r.status IN ('running', 'waiting', 'booting');

-- Issue summary with run counts
SELECT 
  id,
  title,
  status,
  run_count
FROM issues_v
ORDER BY run_count DESC;
```

### Run Queries

```sql
-- Currently running
SELECT issue_id, run_id, agent, status
FROM runs
WHERE status = 'running';

-- Waiting runs needing attention
SELECT 
  issue_id,
  run_id,
  hex_id,
  updated_at
FROM runs
WHERE status IN ('waiting', 'rate_limited')
ORDER BY updated_at DESC;

-- Runs with PRs
SELECT 
  issue_id,
  run_id,
  pr_url,
  status
FROM runs
WHERE pr_url IS NOT NULL;

-- Run history for an issue
SELECT 
  run_id,
  agent,
  status,
  started_at,
  updated_at
FROM runs
WHERE issue_id = 'my-issue'
ORDER BY started_at DESC;

-- Status distribution
SELECT 
  status,
  COUNT(*) as count
FROM runs
GROUP BY status
ORDER BY count DESC;
```

### Analytics Queries

```sql
-- Agent usage
SELECT 
  agent,
  COUNT(*) as runs,
  SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END) as successful
FROM runs
GROUP BY agent;

-- Daily run counts
SELECT 
  DATE(started_at) as day,
  COUNT(*) as runs,
  SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END) as completed
FROM runs
GROUP BY DATE(started_at)
ORDER BY day DESC
LIMIT 14;

-- Average runs per issue
SELECT 
  AVG(run_count) as avg_runs
FROM (
  SELECT issue_id, COUNT(*) as run_count
  FROM runs
  GROUP BY issue_id
);

-- Issues with most runs
SELECT 
  issue_id,
  COUNT(*) as run_count,
  SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END) as done_count
FROM runs
GROUP BY issue_id
ORDER BY run_count DESC
LIMIT 10;
```

### Event Queries

Events require the `--with-events` flag (slower query):

```bash
orch q "SELECT * FROM events" --with-events
```

```sql
-- Events for a specific run
SELECT *
FROM events
WHERE run_id = '20260120-163000'
ORDER BY timestamp;

-- Status transitions
SELECT 
  run_id,
  timestamp,
  name as status
FROM events
WHERE type = 'status'
ORDER BY run_id, timestamp;

-- Time in each phase
SELECT 
  run_id,
  name as phase,
  timestamp
FROM events
WHERE type = 'phase'
ORDER BY run_id, timestamp;

-- PRs created
SELECT 
  e.run_id,
  r.issue_id,
  e.timestamp,
  e.attrs
FROM events e
JOIN runs r ON e.run_id = r.run_id
WHERE e.type = 'artifact' AND e.name = 'pr';
```

## Output Formats

### Table (default)

```bash
orch q "SELECT id, status FROM issues"
```

```
ID          STATUS
my-issue    open
other-task  closed
```

### JSON

```bash
orch q "SELECT id, status FROM issues" --format json
```

```json
[
  {"id": "my-issue", "status": "open"},
  {"id": "other-task", "status": "closed"}
]
```

### TSV (for scripting)

```bash
orch q "SELECT id, status FROM issues" --format tsv
```

```
my-issue    open
other-task  closed
```

## Integration Examples

### Shell scripts

```bash
# Get running run IDs
RUNNING=$(orch q "SELECT hex_id FROM runs WHERE status='running'" --format tsv)

# Iterate over waiting runs
orch q "SELECT hex_id FROM runs WHERE status='waiting'" --format tsv | while read id; do
  echo "Run $id is waiting"
done
```

### jq integration

```bash
# Get PR URLs
orch q "SELECT pr_url FROM runs WHERE pr_url IS NOT NULL" --format json | \
  jq -r '.[].pr_url'

# Count by status
orch q "SELECT status, COUNT(*) as n FROM runs GROUP BY status" --format json | \
  jq -r '.[] | "\(.status): \(.n)"'
```

### Export to CSV

```bash
# Using tsv + sed
orch q "SELECT * FROM issues" --format tsv | sed 's/\t/,/g' > issues.csv
```

## Tips

### Performance

- Use `--with-events` only when needed (loads all events into memory)
- Limit results for large datasets: `LIMIT 100`
- Use specific columns instead of `SELECT *`

### Debugging queries

```bash
# See schema
orch schema

# Test query structure
orch q "SELECT * FROM issues LIMIT 1" --format json
```

### Common patterns

```sql
-- COALESCE for null handling
SELECT COALESCE(pr_url, 'no PR') FROM runs;

-- Date filtering
SELECT * FROM runs
WHERE started_at > datetime('now', '-7 days');

-- Pattern matching
SELECT * FROM issues
WHERE title LIKE '%bug%';
```
