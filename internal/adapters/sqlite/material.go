package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/materials"
	"personaldesk/internal/application/queries"
)

func (s *Store) WithinMaterials(ctx context.Context, action func(materials.Tx) error) error {
	return s.withinWrite(ctx, func(tx *sql.Tx) error { return action(&materialTx{receiveTx{tx}}) })
}

type materialTx struct{ receiveTx }

func (t *materialTx) FindMaterialReceipt(ctx context.Context, client, key string) (materials.Receipt, bool, error) {
	var r materials.Receipt
	var data string
	err := t.tx.QueryRowContext(ctx, `SELECT request_hash,response_status,response_json FROM mutation_receipts WHERE client_id=? AND operation=? AND idempotency_key=?`, client, materials.Operation, key).Scan(&r.RequestHash, &r.Status, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	err = json.Unmarshal([]byte(data), &r.Result)
	return r, true, err
}
func (t *materialTx) MaterialTask(ctx context.Context, id string) (materials.TaskState, error) {
	var r materials.TaskState
	var original sql.NullString
	var genuine int
	err := t.tx.QueryRowContext(ctx, `SELECT t.revision,c.materials_json,(SELECT count(*) FROM task_events e WHERE e.task_id=t.id AND e.kind='task.created') FROM tasks t LEFT JOIN task_creation c ON c.task_id=t.id WHERE t.id=?`, id).Scan(&r.Revision, &original, &genuine)
	if errors.Is(err, sql.ErrNoRows) {
		return r, failure.New("not_found")
	}
	if err != nil {
		return r, err
	}
	if !original.Valid || genuine < 1 {
		return r, failure.New("service_unavailable")
	}
	var associations []queries.MaterialAssociation
	if err = json.Unmarshal([]byte(original.String), &associations); err != nil {
		return r, err
	}
	err = t.tx.QueryRowContext(ctx, `SELECT count(*) FROM task_materials WHERE task_id=?`, id).Scan(&r.Associations)
	r.Associations += len(associations)
	return r, err
}
func (t *materialTx) InsertMaterial(ctx context.Context, r materials.Record) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO task_materials(id,task_id,content_sha256,byte_count,media_type,label,uploaded_client_id,uploaded_role,uploaded_at,task_revision) VALUES(?,?,?,?,?,?,?,?,?,?)`, r.ID, r.TaskID, r.Digest, r.ByteCount, r.MediaType, r.Label, r.AppliedBy.ClientID, r.AppliedBy.Role, r.UploadedAt, r.TaskRevision)
	if err != nil {
		return err
	}
	changed, err := t.tx.ExecContext(ctx, `UPDATE tasks SET revision=?,updated_at=? WHERE id=? AND revision=?`, r.TaskRevision, r.UploadedAt, r.TaskID, r.TaskRevision-1)
	if err != nil {
		return err
	}
	n, err := changed.RowsAffected()
	if err == nil && n != 1 {
		return failure.New("service_unavailable")
	}
	return err
}
func (t *materialTx) InsertMaterialReceipt(ctx context.Context, client, key string, r materials.Receipt) error {
	data, err := json.Marshal(r.Result)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO mutation_receipts(client_id,operation,idempotency_key,request_hash,response_status,response_json) VALUES(?,?,?,?,?,?)`, client, materials.Operation, key, r.RequestHash, r.Status, string(data))
	return err
}
func (s *Store) LoadMaterial(ctx context.Context, id string) (queries.MaterialRecord, int64, error) {
	var r queries.MaterialRecord
	var revision int64
	tx, err := s.readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return r, 0, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT workspace_revision FROM workspace_meta WHERE singleton=1`).Scan(&revision); err != nil {
		return r, 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id,content_sha256,byte_count,media_type FROM task_materials WHERE id=?`, id).Scan(&r.ID, &r.Digest, &r.ByteCount, &r.MediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return r, 0, failure.New("not_found")
	}
	if err != nil {
		return r, 0, err
	}
	return r, revision, tx.Commit()
}
