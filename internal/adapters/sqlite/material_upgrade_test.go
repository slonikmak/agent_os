package sqlite

import (
	"context"
	"encoding/json"
	"personaldesk/internal/application/intake"
	"personaldesk/migrations"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestReleasedSchema3UpgradePreservesFactsAndBackup(t *testing.T) {
	ctx := context.Background()
	catalog, _ := migrations.Embedded()
	entries := catalog.Entries()[:3]
	manifest, _ := json.Marshal(migrations.Manifest{Version: 1, Migrations: entries})
	files := fstest.MapFS{}
	for _, entry := range entries {
		data, _ := migrations.Files.ReadFile(entry.File)
		files[entry.File] = &fstest.MapFile{Data: data}
	}
	old, err := migrations.NewCatalog(files, manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := database(t)
	s, err := OpenCatalog(ctx, path, old, nil)
	if err != nil {
		t.Fatal(err)
	}
	received, _, err := intake.NewReceiver(s, nil).ReceiveMessage(ctx, "receive", receiveInput())
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := intake.NewTaskCreator(s, nil).CreateTask(ctx, received.Message.ID, "create", createInput())
	if err != nil {
		t.Fatal(err)
	}
	before := creationFacts(t, s)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	backup := database(t)
	backups := 0
	s, err = OpenCatalog(ctx, path, catalog, func(ctx context.Context, state State) error {
		if state.Version != 3 {
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
	if backups != 1 || before != creationFacts(t, s) {
		t.Fatal("upgrade changed prior facts")
	}
	state, err := InspectImage(ctx, backup, catalog)
	if err != nil || state.Version != 3 {
		t.Fatal("backup not schema3", state, err)
	}
	replay, _, err := intake.NewTaskCreator(s, nil).CreateTask(ctx, received.Message.ID, "create", createInput())
	if err != nil || !reflect.DeepEqual(created, replay) {
		t.Fatal("old receipt changed")
	}
	task, err := s.LoadTaskDetails(ctx, created.Task.ID)
	if err != nil || !reflect.DeepEqual(task.Task, created.Task) {
		t.Fatal("upgrade changed original associations", err)
	}
}
