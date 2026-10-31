package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
)

// LoadTaskDetails reads the revision, snapshot, creation and immutable events in
// one committed read transaction. Legacy board summaries remain readable only
// as summaries: a missing creation row is never filled with invented facts.
func (s *Store) LoadTaskDetails(ctx context.Context, id string) (queries.TaskDetails, error) {
	result := queries.TaskDetails{Task: queries.Task{Timeline: []queries.TaskEvent{}, Materials: []queries.MaterialAssociation{}}}
	tx, err := s.readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT workspace_revision FROM workspace_meta WHERE singleton=1`).Scan(&result.WorkspaceRevision); err != nil {
		return result, err
	}
	t := &result.Task
	err = tx.QueryRowContext(ctx, `SELECT id,kind,status,title,summary,next_action,next_actor,revision,updated_at FROM tasks WHERE id=?`, id).Scan(&t.ID, &t.Kind, &t.Status, &t.Title, &t.Summary, &t.NextAction, &t.NextActor, &t.Revision, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure.New("not_found")
	}
	if err != nil {
		return result, err
	}
	p := &queries.Provenance{}
	var materials string
	err = tx.QueryRowContext(ctx, `SELECT created_at,inbox_message_id,submitted_client_id,submitted_role,materials_json FROM task_creation WHERE task_id=?`, id).Scan(&t.CreatedAt, &p.MessageID, &p.SubmittedBy.ClientID, &p.SubmittedBy.Role, &materials)
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure.New("service_unavailable")
	}
	if err != nil {
		return result, err
	}
	t.Provenance = p
	if err = json.Unmarshal([]byte(materials), &t.Materials); err != nil {
		return result, err
	}
	materialRows, err := tx.QueryContext(ctx, `SELECT id,label FROM task_materials WHERE task_id=? ORDER BY task_revision`, id)
	if err != nil {
		return result, err
	}
	for materialRows.Next() {
		var materialID string
		var label *string
		if err = materialRows.Scan(&materialID, &label); err != nil {
			materialRows.Close()
			return result, err
		}
		t.Materials = append(t.Materials, queries.MaterialAssociation{Coordinate: queries.ArtifactCoordinate{Kind: "reference", Coordinate: "material:" + materialID, Label: label}, MaterialID: &materialID, Availability: "available"})
	}
	if err = closeRows(materialRows); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,at,reason,inbox_message_id,submitted_client_id,submitted_role,applied_client_id,applied_role FROM task_events WHERE task_id=? ORDER BY at,id`, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var event queries.TaskEvent
		if err = rows.Scan(&event.ID, &event.Kind, &event.At, &event.Reason, &event.Provenance.MessageID, &event.Provenance.SubmittedBy.ClientID, &event.Provenance.SubmittedBy.Role, &event.AppliedBy.ClientID, &event.AppliedBy.Role); err != nil {
			rows.Close()
			return result, err
		}
		t.Timeline = append(t.Timeline, event)
	}
	if err = closeRows(rows); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
