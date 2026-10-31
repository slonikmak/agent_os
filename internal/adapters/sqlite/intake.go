package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
)

func (s *Store) Within(ctx context.Context, action func(intake.ReceiveTx) error) error {
	return s.withinWrite(ctx, func(tx *sql.Tx) error { return action(&receiveTx{tx}) })
}

// Both typed command ports share this gate, connection, deadline and fault seam.
func (s *Store) withinWrite(ctx context.Context, action func(*sql.Tx) error) error {
	select {
	case s.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.writeGate }()
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = action(tx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if s.beforeCommit != nil {
		if err = s.beforeCommit(); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

type receiveTx struct{ tx *sql.Tx }

func (t *receiveTx) FindReceipt(ctx context.Context, client, operation, key string) (intake.Receipt, bool, error) {
	var receipt intake.Receipt
	var data string
	err := t.tx.QueryRowContext(ctx, `SELECT request_hash,response_status,response_json FROM mutation_receipts WHERE client_id=? AND operation=? AND idempotency_key=?`, client, operation, key).Scan(&receipt.RequestHash, &receipt.Status, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, false, nil
	}
	if err != nil {
		return receipt, false, err
	}
	err = json.Unmarshal([]byte(data), &receipt.Result)
	return receipt, true, err
}
func (t *receiveTx) FindMessage(ctx context.Context, source, scope, identity string) (intake.SavedMessage, bool, error) {
	var id string
	err := t.tx.QueryRowContext(ctx, `SELECT id FROM inbox_messages WHERE source=? AND scope=? AND message_id=?`, source, scope, identity).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return intake.SavedMessage{}, false, nil
	}
	if err != nil {
		return intake.SavedMessage{}, false, err
	}
	message, hash, err := readMessage(ctx, t.tx, id)
	// Recognized legacy metadata has no original payload. Its claimed identity
	// remains reserved, so redelivery conservatively conflicts instead of filling it.
	var f *failure.Failure
	if errors.As(err, &f) && f.Code == "service_unavailable" {
		return intake.SavedMessage{}, true, nil
	}
	return intake.SavedMessage{IdentityHash: hash, Message: message}, true, err
}
func (t *receiveTx) AllocateMessage(ctx context.Context) (string, int64, error) {
	var sequence int64
	err := t.tx.QueryRowContext(ctx, `UPDATE workspace_counters SET message_sequence=message_sequence+1 WHERE singleton=1 AND message_sequence<? RETURNING message_sequence`, queries.MaxRevision).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, failure.New("service_unavailable")
	}
	if err != nil {
		return "", 0, err
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return "", 0, err
	}
	return "I-" + hex.EncodeToString(id[:]), sequence, nil
}
func (t *receiveTx) InsertMessage(ctx context.Context, m queries.InboxMessage, hash string, sequence int64) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO inbox_messages(id,source,scope,message_id,kind,status,sender,occurred_at,received_at,revision) VALUES(?,?,?,?,?,?,?,?,?,?)`, m.ID, m.Source, m.Scope, m.MessageID, m.Kind, m.Status, m.Sender, m.OccurredAt, m.ReceivedAt, m.Revision)
	if err != nil {
		return err
	}
	artifacts, err := json.Marshal(m.Artifacts)
	if err != nil {
		return err
	}
	var proposed *string
	if m.ProposedTaskRef != nil {
		data, e := json.Marshal(m.ProposedTaskRef)
		if e != nil {
			return e
		}
		text := string(data)
		proposed = &text
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO inbox_payloads(message_id,identity_hash,original_text,source_url,artifacts_json,proposed_task_json,submitted_client_id,submitted_role,receipt_sequence) VALUES(?,?,?,?,?,?,?,?,?)`, m.ID, hash, m.Text, m.SourceURL, string(artifacts), proposed, m.SubmittedBy.ClientID, string(m.SubmittedBy.Role), sequence)
	return err
}
func (t *receiveTx) Revision(ctx context.Context, advance bool) (int64, error) {
	var revision int64
	query := `SELECT workspace_revision FROM workspace_meta WHERE singleton=1`
	var args []any
	if advance {
		query = `UPDATE workspace_meta SET workspace_revision=workspace_revision+1 WHERE singleton=1 AND workspace_revision<? RETURNING workspace_revision`
		args = []any{queries.MaxRevision}
	}
	err := t.tx.QueryRowContext(ctx, query, args...).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, failure.New("service_unavailable")
	}
	return revision, err
}
func (t *receiveTx) InsertReceipt(ctx context.Context, client, operation, key string, r intake.Receipt) error {
	data, err := json.Marshal(r.Result)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO mutation_receipts(client_id,operation,idempotency_key,request_hash,response_status,response_json) VALUES(?,?,?,?,?,?)`, client, operation, key, r.RequestHash, r.Status, string(data))
	return err
}
func readMessage(ctx context.Context, tx *sql.Tx, id string) (queries.InboxMessage, string, error) {
	var m queries.InboxMessage
	var hash, artifacts, client, role sql.NullString
	var text, proposed sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT m.id,m.source,m.scope,m.kind,m.status,m.sender,m.occurred_at,m.received_at,m.revision,m.message_id,p.identity_hash,p.original_text,p.source_url,p.artifacts_json,p.proposed_task_json,p.submitted_client_id,p.submitted_role FROM inbox_messages m LEFT JOIN inbox_payloads p ON p.message_id=m.id WHERE m.id=?`, id).Scan(&m.ID, &m.Source, &m.Scope, &m.Kind, &m.Status, &m.Sender, &m.OccurredAt, &m.ReceivedAt, &m.Revision, &m.MessageID, &hash, &text, &m.SourceURL, &artifacts, &proposed, &client, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return m, "", failure.New("not_found")
	}
	if err != nil {
		return m, "", err
	}
	if !hash.Valid || !text.Valid || !client.Valid || !role.Valid {
		return m, "", failure.New("service_unavailable")
	}
	m.Text = text.String
	m.SubmittedBy = access.Principal{ClientID: client.String, Role: access.Role(role.String)}
	if err = json.Unmarshal([]byte(artifacts.String), &m.Artifacts); err != nil {
		return m, "", err
	}
	if proposed.Valid {
		if err = json.Unmarshal([]byte(proposed.String), &m.ProposedTaskRef); err != nil {
			return m, "", err
		}
	}
	var resolution queries.Resolution
	err = tx.QueryRowContext(ctx, `SELECT id,action,reason,task_id,applied_client_id,applied_role,applied_at FROM inbox_resolutions WHERE message_id=? AND terminal=1`, id).Scan(&resolution.ID, &resolution.Action, &resolution.Reason, &resolution.TaskID, &resolution.AppliedBy.ClientID, &resolution.AppliedBy.Role, &resolution.AppliedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return m, "", err
	}
	if err == nil {
		m.Resolution = &resolution
	}
	return m, hash.String, nil
}
func (s *Store) LoadInboxMessage(ctx context.Context, id string) (queries.InboxDetails, error) {
	var result queries.InboxDetails
	tx, err := s.readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT workspace_revision FROM workspace_meta WHERE singleton=1`).Scan(&result.WorkspaceRevision); err != nil {
		return result, err
	}
	result.Message, _, err = readMessage(ctx, tx, id)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
