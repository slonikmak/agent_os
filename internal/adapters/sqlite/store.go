// Package sqlite owns SQL, migration preflight, and database resources.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
	"personaldesk/internal/application/queries"
)

var ErrUnsupported = errors.New("unsupported database")

type Store struct {
	writer, readers *sql.DB
	writeGate       chan struct{}
	beforeCommit    func() error // private fault seam, exercised only by adapter tests
}

func dsn(path, mode string, readOnly bool) string {
	slashPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	u := url.URL{Scheme: "file", Path: slashPath}
	q := u.Query()
	q.Set("mode", mode)
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(2500)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "synchronous(FULL)")
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func (s *Store) Close() error { return errors.Join(s.readers.Close(), s.writer.Close()) }
func (s *Store) LoadWorkspace(ctx context.Context) (queries.WorkspaceSnapshot, error) {
	snapshot := queries.WorkspaceSnapshot{Tasks: []queries.TaskSummary{}, Inbox: []queries.InboxMetadata{}, Sources: []queries.SourceHealth{}}
	tx, err := s.readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT workspace_id,workspace_revision,created_at FROM workspace_meta WHERE singleton=1").Scan(&snapshot.WorkspaceID, &snapshot.WorkspaceRevision, &snapshot.CreatedAt); err != nil {
		return snapshot, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,kind,status,title,summary,next_action,next_actor,revision,updated_at FROM tasks ORDER BY id")
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item queries.TaskSummary
		if err = rows.Scan(&item.ID, &item.Kind, &item.Status, &item.Title, &item.Summary, &item.NextAction, &item.NextActor, &item.Revision, &item.UpdatedAt); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Tasks = append(snapshot.Tasks, item)
	}
	if err = closeRows(rows); err != nil {
		return snapshot, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT m.id,m.source,m.scope,m.kind,m.status,m.sender,m.occurred_at,m.received_at,m.revision FROM inbox_messages m LEFT JOIN inbox_payloads p ON p.message_id=m.id ORDER BY p.receipt_sequence DESC,m.received_at DESC,m.id")
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item queries.InboxMetadata
		if err = rows.Scan(&item.ID, &item.Source, &item.Scope, &item.Kind, &item.Status, &item.Sender, &item.OccurredAt, &item.ReceivedAt, &item.Revision); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Inbox = append(snapshot.Inbox, item)
	}
	if err = closeRows(rows); err != nil {
		return snapshot, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT source,scope,status,last_attempt_at,last_success_at FROM source_health ORDER BY source,scope")
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item queries.SourceHealth
		if err = rows.Scan(&item.Source, &item.Scope, &item.Status, &item.LastAttemptAt, &item.LastSuccessAt); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Sources = append(snapshot.Sources, item)
	}
	if err = closeRows(rows); err != nil {
		return snapshot, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM inbox_messages),(SELECT count(*) FROM inbox_messages WHERE status IN ('pending','needs_attention')),(SELECT count(*) FROM tasks WHERE status='review')`).Scan(&snapshot.Counts.Tasks, &snapshot.Counts.Inbox, &snapshot.Counts.UnprocessedInbox, &snapshot.Counts.NeedsAttention); err != nil {
		return snapshot, err
	}
	if snapshot.WorkspaceID == "" || snapshot.WorkspaceRevision < 1 || snapshot.WorkspaceRevision > queries.MaxRevision {
		return snapshot, fmt.Errorf("invalid persisted metadata")
	}
	return snapshot, tx.Commit()
}
func closeRows(rows *sql.Rows) error { return errors.Join(rows.Err(), rows.Close()) }
