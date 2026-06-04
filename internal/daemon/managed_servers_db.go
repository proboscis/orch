package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/xdg"
	_ "modernc.org/sqlite"
)

const managedServerTimestampLayout = time.RFC3339Nano

type managedServerStore struct {
	db *sql.DB
}

type managedServerRecord struct {
	RepoID      string
	ProjectRoot string
	PID         int
	Port        int
	LogPath     string
	StartedAt   time.Time
	LastHealthy time.Time
}

func newManagedServerStore(dbPath string) (*managedServerStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create daemon db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open daemon db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	store := &managedServerStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *managedServerStore) migrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("managed server store is not initialized")
	}
	if err := s.migrateManagedServersTable(); err != nil {
		return err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS events (
		seq            INTEGER PRIMARY KEY AUTOINCREMENT,
		stream_type    TEXT NOT NULL,
		stream_id      TEXT NOT NULL,
		stream_version INTEGER NOT NULL,
		event_type     TEXT NOT NULL,
		payload_json   TEXT NOT NULL,
		metadata_json  TEXT,
		created_at     TEXT NOT NULL
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_events_stream_version ON events(stream_type, stream_id, stream_version);

	CREATE TABLE IF NOT EXISTS run_state_projection (
		run_id       TEXT PRIMARY KEY,
		project_id   TEXT NOT NULL,
		issue_id     TEXT NOT NULL,
		status       TEXT NOT NULL,
		updated_at   TEXT NOT NULL,
		summary_json TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_run_state_projection_project_status ON run_state_projection(project_id, status);

	CREATE TABLE IF NOT EXISTS issue_state_projection (
		issue_id     TEXT PRIMARY KEY,
		project_id   TEXT NOT NULL,
		status       TEXT NOT NULL,
		updated_at   TEXT NOT NULL,
		summary_json TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_issue_state_projection_project_status ON issue_state_projection(project_id, status);

	CREATE TABLE IF NOT EXISTS idempotency_keys (
		request_id    TEXT PRIMARY KEY,
		response_json TEXT NOT NULL,
		created_at    TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_idempotency_keys_created_at ON idempotency_keys(created_at);

	CREATE TABLE IF NOT EXISTS outbox (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		kind         TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		status       TEXT NOT NULL,
		attempts     INTEGER NOT NULL DEFAULT 0,
		updated_at   TEXT NOT NULL,
		created_at   TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_outbox_status_updated ON outbox(status, updated_at);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to migrate managed server store: %w", err)
	}

	return nil
}

func (s *managedServerStore) migrateManagedServersTable() error {
	const schema = `
	CREATE TABLE IF NOT EXISTS managed_servers (
		repo_id      TEXT PRIMARY KEY,
		project_root TEXT NOT NULL,
		pid          INTEGER NOT NULL,
		port         INTEGER NOT NULL,
		log_path     TEXT,
		started_at   TEXT NOT NULL,
		last_healthy TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_managed_servers_port ON managed_servers(port);
	CREATE INDEX IF NOT EXISTS idx_managed_servers_project_root ON managed_servers(project_root);
	`

	columns, err := s.tableColumns("managed_servers")
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create managed_servers table: %w", err)
		}
		return nil
	}
	if _, ok := columns["repo_id"]; ok {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to ensure managed_servers indexes: %w", err)
		}
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin managed_servers migration: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`
		CREATE TABLE managed_servers_v2 (
			repo_id      TEXT PRIMARY KEY,
			project_root TEXT NOT NULL,
			pid          INTEGER NOT NULL,
			port         INTEGER NOT NULL,
			log_path     TEXT,
			started_at   TEXT NOT NULL,
			last_healthy TEXT
		)
	`); err != nil {
		return fmt.Errorf("create managed_servers_v2: %w", err)
	}

	rows, err := tx.Query(`SELECT project_root, pid, port, log_path, started_at, last_healthy FROM managed_servers`)
	if err != nil {
		return fmt.Errorf("read legacy managed_servers rows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			projectRoot string
			pid         int
			port        int
			logPath     sql.NullString
			startedAt   string
			lastHealthy sql.NullString
		)
		if err := rows.Scan(&projectRoot, &pid, &port, &logPath, &startedAt, &lastHealthy); err != nil {
			return fmt.Errorf("scan legacy managed_servers row: %w", err)
		}

		repoID, err := xdg.RepoIDStrict(projectRoot)
		if err != nil || strings.TrimSpace(repoID.String()) == "" {
			continue
		}

		if _, err := tx.Exec(`
			INSERT INTO managed_servers_v2 (repo_id, project_root, pid, port, log_path, started_at, last_healthy)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_id) DO UPDATE SET
				project_root = excluded.project_root,
				pid = excluded.pid,
				port = excluded.port,
				log_path = excluded.log_path,
				started_at = excluded.started_at,
				last_healthy = excluded.last_healthy
		`, strings.TrimSpace(repoID.String()), projectRoot, pid, port, nullStringValue(logPath), startedAt, nullStringValue(lastHealthy)); err != nil {
			return fmt.Errorf("migrate managed_servers row for %s: %w", projectRoot, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy managed_servers rows: %w", err)
	}

	if _, err := tx.Exec(`DROP TABLE managed_servers`); err != nil {
		return fmt.Errorf("drop legacy managed_servers table: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE managed_servers_v2 RENAME TO managed_servers`); err != nil {
		return fmt.Errorf("rename managed_servers_v2: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_managed_servers_port ON managed_servers(port)`); err != nil {
		return fmt.Errorf("create managed_servers port index: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_managed_servers_project_root ON managed_servers(project_root)`); err != nil {
		return fmt.Errorf("create managed_servers project_root index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit managed_servers migration: %w", err)
	}
	tx = nil
	return nil
}

func (s *managedServerStore) tableColumns(table string) (map[string]struct{}, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return nil, fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table_info(%s): %w", table, err)
	}
	return columns, nil
}

func (s *managedServerStore) Upsert(record managedServerRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("managed server store is not initialized")
	}

	if strings.TrimSpace(record.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	if record.ProjectRoot == "" {
		return fmt.Errorf("project_root is required")
	}
	if record.PID <= 0 {
		return fmt.Errorf("pid must be positive")
	}
	if record.Port <= 0 {
		return fmt.Errorf("port must be positive")
	}

	startedAt := record.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	query := `
	INSERT INTO managed_servers (repo_id, project_root, pid, port, log_path, started_at, last_healthy)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(repo_id) DO UPDATE SET
		project_root = excluded.project_root,
		pid = excluded.pid,
		port = excluded.port,
		log_path = excluded.log_path,
		started_at = excluded.started_at,
		last_healthy = excluded.last_healthy
	`

	_, err := s.db.Exec(
		query,
		record.RepoID,
		record.ProjectRoot,
		record.PID,
		record.Port,
		record.LogPath,
		startedAt.Format(managedServerTimestampLayout),
		nullableTimestamp(record.LastHealthy),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert managed server for %s: %w", record.RepoID, err)
	}

	return nil
}

func (s *managedServerStore) Delete(repoID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("managed server store is not initialized")
	}
	if repoID == "" {
		return nil
	}

	if _, err := s.db.Exec(`DELETE FROM managed_servers WHERE repo_id = ?`, repoID); err != nil {
		return fmt.Errorf("failed to delete managed server for %s: %w", repoID, err)
	}
	return nil
}

func (s *managedServerStore) UpdateLastHealthy(repoID string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("managed server store is not initialized")
	}
	if repoID == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}

	if _, err := s.db.Exec(`UPDATE managed_servers SET last_healthy = ? WHERE repo_id = ?`, at.Format(managedServerTimestampLayout), repoID); err != nil {
		return fmt.Errorf("failed to update last_healthy for %s: %w", repoID, err)
	}
	return nil
}

func (s *managedServerStore) List() ([]managedServerRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("managed server store is not initialized")
	}

	rows, err := s.db.Query(`SELECT repo_id, project_root, pid, port, log_path, started_at, last_healthy FROM managed_servers ORDER BY repo_id`)
	if err != nil {
		return nil, fmt.Errorf("failed to list managed servers: %w", err)
	}
	defer rows.Close()

	records := make([]managedServerRecord, 0)
	for rows.Next() {
		var rec managedServerRecord
		var startedAt string
		var lastHealthy sql.NullString

		if err := rows.Scan(&rec.RepoID, &rec.ProjectRoot, &rec.PID, &rec.Port, &rec.LogPath, &startedAt, &lastHealthy); err != nil {
			return nil, fmt.Errorf("failed to scan managed server row: %w", err)
		}

		rec.StartedAt = parseManagedServerTimestamp(startedAt)
		rec.LastHealthy = parseNullableManagedServerTimestamp(lastHealthy)
		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while iterating managed servers: %w", err)
	}

	return records, nil
}

func (s *managedServerStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func nullableTimestamp(ts time.Time) sql.NullString {
	if ts.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: ts.Format(managedServerTimestampLayout), Valid: true}
}

func parseManagedServerTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	ts, err := time.Parse(managedServerTimestampLayout, value)
	if err != nil {
		return time.Time{}
	}
	return ts
}

func parseNullableManagedServerTimestamp(value sql.NullString) time.Time {
	if !value.Valid || value.String == "" {
		return time.Time{}
	}
	return parseManagedServerTimestamp(value.String)
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func (s *SocketServer) initManagedServerStore() error {
	if s.managedServerStore != nil {
		return nil
	}

	if err := xdg.EnsureDataDir(); err != nil {
		return fmt.Errorf("failed to create data directory for daemon db: %w", err)
	}

	store, err := newManagedServerStore(xdg.DaemonDBPath())
	if err != nil {
		return err
	}

	s.managedServerStore = store
	return nil
}

func (s *SocketServer) closeManagedServerStore() {
	if s.managedServerStore == nil {
		return
	}

	if err := s.managedServerStore.Close(); err != nil && s.logger != nil {
		s.logger.Printf("warning: failed to close managed server store: %v", err)
	}
	s.managedServerStore = nil
}

func (s *SocketServer) persistManagedServerStart(srv *managedServer) error {
	if s.managedServerStore == nil || srv == nil {
		return nil
	}

	record := managedServerRecord{
		RepoID:      srv.RepoID,
		ProjectRoot: srv.ProjectRoot,
		PID:         serverPID(srv),
		Port:        srv.Port,
		LogPath:     srv.LogPath,
		StartedAt:   srv.StartTime,
		LastHealthy: srv.LastHealthy,
	}
	return s.managedServerStore.Upsert(record)
}

func (s *SocketServer) deleteManagedServerRecord(repoID string) {
	if s.managedServerStore == nil || repoID == "" {
		return
	}

	if err := s.managedServerStore.Delete(repoID); err != nil && s.logger != nil {
		s.logger.Printf("warning: failed to delete managed server record for %s: %v", repoID, err)
	}
}

func (s *SocketServer) updateManagedServerHealth(repoID string, at time.Time) {
	if s.managedServerStore == nil || repoID == "" {
		return
	}

	if err := s.managedServerStore.UpdateLastHealthy(repoID, at); err != nil && s.logger != nil {
		s.logger.Printf("warning: failed to update managed server health for %s: %v", repoID, err)
	}
}

func (s *SocketServer) reconcileManagedServersOnStartup() error {
	if s.managedServerStore == nil {
		return nil
	}

	records, err := s.managedServerStore.List()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	adoptedPorts := make(map[int]string, len(records))
	for _, record := range records {
		s.reconcileManagedServerRecord(record, adoptedPorts)
	}

	return nil
}

func (s *SocketServer) reconcileManagedServerRecord(record managedServerRecord, adoptedPorts map[int]string) {
	if record.RepoID == "" || record.ProjectRoot == "" || record.PID <= 0 || record.Port <= 0 {
		s.deleteManagedServerRecord(record.RepoID)
		return
	}

	if repoID, exists := adoptedPorts[record.Port]; exists && repoID != record.RepoID {
		if s.logger != nil {
			s.logger.Printf("startup recovery: port %d already adopted for %s; removing duplicate managed server record for %s", record.Port, repoID, record.RepoID)
		}
		if err := s.terminateServerProcessByPID(record.PID, 5*time.Second); err != nil && s.logger != nil {
			s.logger.Printf("warning: failed to terminate duplicate server process pid=%d for %s: %v", record.PID, record.RepoID, err)
		}
		s.deleteManagedServerRecord(record.RepoID)
		return
	}

	if !IsProcessRunning(record.PID) {
		if s.logger != nil {
			s.logger.Printf("startup recovery: removing stale managed server record for %s (pid=%d dead)", record.RepoID, record.PID)
		}
		s.deleteManagedServerRecord(record.RepoID)
		return
	}

	client := agent.NewOpenCodeClient(record.Port)
	probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	healthy := client.IsServerRunningForWorktree(probeCtx, record.ProjectRoot)
	cancel()

	if !healthy {
		if s.logger != nil {
			s.logger.Printf("startup recovery: managed server pid=%d for %s unhealthy on port %d; terminating", record.PID, record.RepoID, record.Port)
		}
		if err := s.terminateServerProcessByPID(record.PID, 5*time.Second); err != nil {
			if s.logger != nil {
				s.logger.Printf("warning: failed to terminate unhealthy managed server pid=%d for %s: %v", record.PID, record.RepoID, err)
			}
			return
		}
		s.deleteManagedServerRecord(record.RepoID)
		return
	}

	lastHealthy := record.LastHealthy
	if lastHealthy.IsZero() {
		lastHealthy = time.Now()
	}

	s.openCodeServers[record.RepoID] = &managedServer{
		RepoID:      record.RepoID,
		ProjectRoot: record.ProjectRoot,
		Port:        record.Port,
		PID:         record.PID,
		StartTime:   record.StartedAt,
		LastHealthy: lastHealthy,
		LogPath:     record.LogPath,
		Adopted:     true,
	}
	adoptedPorts[record.Port] = record.RepoID
	s.updateManagedServerHealth(record.RepoID, lastHealthy)

	if s.logger != nil {
		s.logger.Printf("startup recovery: re-adopted opencode server for %s on port %d (pid: %d)", record.RepoID, record.Port, record.PID)
	}
}

func (s *SocketServer) terminateServerProcessByPID(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := process.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	hardDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(hardDeadline) {
		if !IsProcessRunning(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	if IsProcessRunning(pid) {
		return fmt.Errorf("process %d still running after SIGKILL", pid)
	}

	return nil
}
