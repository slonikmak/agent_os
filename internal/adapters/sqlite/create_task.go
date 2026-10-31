package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
)

func (s *Store) WithinCreateTask(ctx context.Context, action func(intake.CreateTaskTx) error) error {
	return s.withinWrite(ctx, func(tx *sql.Tx) error { return action(&createTaskTx{receiveTx{tx}}) })
}

type createTaskTx struct{ receiveTx }

func (t *createTaskTx) FindCreateTaskReceipt(ctx context.Context, client, operation, key string) (intake.CreateTaskReceipt, bool, error) {
	var receipt intake.CreateTaskReceipt
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
func (t *createTaskTx) GetMessage(ctx context.Context, id string) (queries.InboxMessage, error) {
	m, _, err := readMessage(ctx, t.tx, id)
	return m, err
}
func (t *createTaskTx) AllocateCreation(ctx context.Context) (string, string, string, error) {
	var sequence int64
	err := t.tx.QueryRowContext(ctx, `UPDATE workspace_counters SET task_sequence=task_sequence+1,event_sequence=event_sequence+1,resolution_sequence=resolution_sequence+1 WHERE singleton=1 AND task_sequence<? AND event_sequence<? AND resolution_sequence<? RETURNING task_sequence`, queries.MaxRevision, queries.MaxRevision, queries.MaxRevision).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", failure.New("service_unavailable")
	}
	if err != nil {
		return "", "", "", err
	}
	// Opaque random IDs avoid relying on sequence values absent from legacy rows.
	var ids [48]byte
	if _, err = rand.Read(ids[:]); err != nil {
		return "", "", "", err
	}
	return "T-" + hex.EncodeToString(ids[:16]), "E-" + hex.EncodeToString(ids[16:32]), "R-" + hex.EncodeToString(ids[32:]), nil
}
func (t *createTaskTx) InsertTask(ctx context.Context, task queries.Task) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO tasks(id,kind,status,title,summary,next_action,next_actor,revision,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, task.ID, task.Kind, task.Status, task.Title, task.Summary, task.NextAction, task.NextActor, task.Revision, task.UpdatedAt)
	if err != nil {
		return err
	}
	materials, err := json.Marshal(task.Materials)
	if err != nil {
		return err
	}
	p := task.Provenance
	_, err = t.tx.ExecContext(ctx, `INSERT INTO task_creation(task_id,created_at,inbox_message_id,submitted_client_id,submitted_role,materials_json) VALUES(?,?,?,?,?,?)`, task.ID, task.CreatedAt, p.MessageID, p.SubmittedBy.ClientID, p.SubmittedBy.Role, string(materials))
	if err != nil {
		return err
	}
	for _, event := range task.Timeline {
		_, err = t.tx.ExecContext(ctx, `INSERT INTO task_events(id,task_id,kind,at,reason,inbox_message_id,submitted_client_id,submitted_role,applied_client_id,applied_role) VALUES(?,?,?,?,?,?,?,?,?,?)`, event.ID, task.ID, event.Kind, event.At, event.Reason, event.Provenance.MessageID, event.Provenance.SubmittedBy.ClientID, event.Provenance.SubmittedBy.Role, event.AppliedBy.ClientID, event.AppliedBy.Role)
		if err != nil {
			return err
		}
	}
	return nil
}
func (t *createTaskTx) ResolveMessage(ctx context.Context, message queries.InboxMessage, hash string) error {
	r := message.Resolution
	_, err := t.tx.ExecContext(ctx, `INSERT INTO inbox_resolutions(id,message_id,action,reason,task_id,applied_client_id,applied_role,applied_at,request_hash,terminal) VALUES(?,?,?,?,?,?,?,?,?,1)`, r.ID, message.ID, r.Action, r.Reason, r.TaskID, r.AppliedBy.ClientID, r.AppliedBy.Role, r.AppliedAt, hash)
	if err != nil {
		return err
	}
	result, err := t.tx.ExecContext(ctx, `UPDATE inbox_messages SET status='applied',revision=revision+1 WHERE id=? AND status='pending' AND revision=? AND revision<?`, message.ID, message.Revision-1, queries.MaxRevision)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err == nil && changed != 1 {
		return failure.New("service_unavailable")
	}
	return err
}
func (t *createTaskTx) InsertCreateTaskReceipt(ctx context.Context, client, operation, key string, receipt intake.CreateTaskReceipt) error {
	data, err := json.Marshal(receipt.Result)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO mutation_receipts(client_id,operation,idempotency_key,request_hash,response_status,response_json) VALUES(?,?,?,?,?,?)`, client, operation, key, receipt.RequestHash, receipt.Status, string(data))
	return err
}
