package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
	"reflect"
	"testing"
)

func TestTaskDetailsRealCreationPassiveConsistencyAndRestart(t *testing.T) {
	s, path, received := seededCreate(t)
	in := createInput()
	summary, action, actor := "Saved summary", "Saved next action", "Owner"
	in.Task.Summary, in.Task.NextAction, in.Task.NextActor = &summary, &action, &actor
	created, _, err := intake.NewTaskCreator(s, nil).CreateTask(context.Background(), received.Message.ID, "create", in)
	if err != nil {
		t.Fatal(err)
	}
	want := queries.TaskDetails{WorkspaceRevision: created.WorkspaceRevision, Task: created.Task}
	before := creationFacts(t, s)
	for range 4 {
		got, err := queries.New(s).GetTaskDetails(context.Background(), created.Task.ID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("committed task drift", got, err)
		}
	}
	if before != creationFacts(t, s) {
		t.Fatal("passive details wrote canonical facts")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := queries.New(s).GetTaskDetails(context.Background(), created.Task.ID)
	if err != nil || !reflect.DeepEqual(got, want) || before != creationFacts(t, s) {
		t.Fatal("restart detail drift", got, err)
	}
}

func TestTaskDetailsRevisionAndFactsShareOneCommittedRead(t *testing.T) {
	s, _, received := seededCreate(t)
	created, _, err := intake.NewTaskCreator(s, nil).CreateTask(context.Background(), received.Message.ID, "create", createInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.writer.Exec(`UPDATE tasks SET title='Revision 3' WHERE id=?`, created.Task.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for revision := 4; revision <= 100; revision++ {
			err := s.withinWrite(context.Background(), func(tx *sql.Tx) error {
				if _, err := tx.Exec(`UPDATE workspace_meta SET workspace_revision=?`, revision); err != nil {
					return err
				}
				_, err := tx.Exec(`UPDATE tasks SET title=? WHERE id=?`, fmt.Sprintf("Revision %d", revision), created.Task.ID)
				return err
			})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for range 150 {
		got, err := queries.New(s).GetTaskDetails(context.Background(), created.Task.ID)
		if err != nil || got.Task.Title != fmt.Sprintf("Revision %d", got.WorkspaceRevision) {
			t.Fatal("mixed committed task/revision", got, err)
		}
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTaskDetailsMissingLegacyCancellationCorruptionAndDeterministicHistory(t *testing.T) {
	s, _, received := seededCreate(t)
	reader := queries.New(s)
	_, err := reader.GetTaskDetails(context.Background(), "T-missing")
	requireCode(t, err, "not_found")
	_, err = s.writer.Exec(`INSERT INTO tasks(id,kind,status,title,revision,updated_at) VALUES('T-legacy','request','new','Legacy summary',1,'2026-10-29T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.GetTaskDetails(context.Background(), "T-legacy")
	requireCode(t, err, "service_unavailable")
	snapshot, err := reader.GetWorkspaceSnapshot(context.Background())
	if err != nil || snapshot.Counts.Tasks != 1 || snapshot.Tasks[0].ID != "T-legacy" {
		t.Fatal("legacy summary was hidden", err)
	}
	created, _, err := intake.NewTaskCreator(s, nil).CreateTask(context.Background(), received.Message.ID, "create", createInput())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reader.GetTaskDetails(ctx, created.Task.ID)
	requireCode(t, err, "service_unavailable")
	for _, id := range []string{"E-z", "E-a"} {
		_, err = s.writer.Exec(`INSERT INTO task_events SELECT ?,task_id,kind,at,reason,inbox_message_id,submitted_client_id,submitted_role,applied_client_id,applied_role FROM task_events WHERE id=?`, id, created.Task.Timeline[0].ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := reader.GetTaskDetails(context.Background(), created.Task.ID)
	if err != nil || len(got.Task.Timeline) != 3 || got.Task.Timeline[0].ID >= got.Task.Timeline[1].ID || got.Task.Timeline[1].ID >= got.Task.Timeline[2].ID {
		t.Fatal("history order not deterministic", got, err)
	}
	if _, err = s.writer.Exec(`UPDATE task_creation SET materials_json='PRIVATE malformed' WHERE task_id=?`, created.Task.ID); err != nil {
		t.Fatal(err)
	}
	_, err = reader.GetTaskDetails(context.Background(), created.Task.ID)
	requireCode(t, err, "internal_error")
}
