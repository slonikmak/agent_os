package sqlite

import (
	"context"
	"reflect"
	"testing"

	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
	"personaldesk/internal/testsupport/migrationfixture"
	"personaldesk/migrations"
)

func TestIndependentReleasedV2UpgradePreservesOriginalReceiptAndLegacySummaries(t *testing.T) {
	ctx := context.Background()
	path := database(t)
	if err := migrationfixture.PopulateV2(path); err != nil {
		t.Fatal(err)
	}
	catalog, _ := migrations.Embedded()
	backup := database(t)
	backups := 0
	s, err := OpenCatalog(ctx, path, catalog, func(ctx context.Context, state State) error {
		if state.Version != 2 {
			t.Fatal(state)
		}
		backups++
		_, err := Backup(ctx, path, backup, catalog)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if backups != 1 {
		t.Fatal("upgrade skipped verified backup")
	}
	state, err := InspectImage(ctx, backup, catalog)
	if err != nil || state.Version != 2 {
		t.Fatal("v2 backup lost", state, err)
	}
	in := receiveInput()
	in.MessageID = "released-v2"
	in.Kind = "external_signal"
	in.Text = "Saved released-v2 original"
	in.Artifacts = []queries.ArtifactCoordinate{}
	received, status, err := intake.NewReceiver(s, nil).ReceiveMessage(ctx, "released-v2", in)
	if err != nil || status != 201 || received.WorkspaceRevision != 43 || received.Message.ID != "I-released-v2" || received.Message.Revision != 1 || received.Message.Text != in.Text || received.Message.SubmittedBy != in.SubmittedBy {
		t.Fatal("released receipt changed", received, status, err)
	}
	before, err := s.LoadInboxMessage(ctx, "I-released-v2")
	if err != nil || !reflect.DeepEqual(before.Message, received.Message) {
		t.Fatal("v2 original changed", err)
	}
	var invented int
	if err = s.writer.QueryRow(`SELECT (SELECT count(*) FROM task_creation)+(SELECT count(*) FROM task_events)+(SELECT count(*) FROM inbox_resolutions)`).Scan(&invented); err != nil || invented != 0 {
		t.Fatal("invented legacy creation facts")
	}
	_, err = s.LoadInboxMessage(ctx, "I-frozen")
	requireCode(t, err, "service_unavailable")
	created, _, err := intake.NewTaskCreator(s, nil).CreateTask(ctx, "I-released-v2", "create", createInput())
	if err != nil || created.WorkspaceRevision != 44 {
		t.Fatal("upgraded input cannot create", err)
	}
	replay, _, err := intake.NewReceiver(s, nil).ReceiveMessage(ctx, "released-v2", in)
	if err != nil || !reflect.DeepEqual(replay, received) {
		t.Fatal("create rewrote legacy receive receipt", err)
	}
	snapshot, err := s.LoadWorkspace(ctx)
	if err != nil || snapshot.Counts.Tasks != 2 || snapshot.Tasks[len(snapshot.Tasks)-1].ID != "T-frozen" || snapshot.Tasks[len(snapshot.Tasks)-1].Revision != 7 {
		t.Fatal("legacy summary changed", snapshot.Tasks, err)
	}
}
