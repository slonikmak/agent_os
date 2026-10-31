package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	files "personaldesk/internal/adapters/materials"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/materials"
	"personaldesk/internal/application/queries"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func materialFixture(t *testing.T) (*Store, *files.Content, string, string) {
	t.Helper()
	s, path, received := seededCreate(t)
	created, _, err := intake.NewTaskCreator(s, nil).CreateTask(context.Background(), received.Message.ID, "create", createInput())
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	content, err := files.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { content.Close() })
	return s, content, path, created.Task.ID
}
func uploadInput() materials.Upload {
	return materials.Upload{ExpectedTaskRevision: 1, MediaType: "text/markdown", Text: "# Exact saved 👋\n<script>inert</script>\n![image](https://example.invalid/a.png)", AppliedBy: createInput().AppliedBy}
}
func TestMaterialUploadRegistryReceiptsOriginalFactsAndRestart(t *testing.T) {
	s, content, path, id := materialFixture(t)
	ctx := context.Background()
	beforeCreation := creationFacts(t, s)
	published := 0
	service := materials.New(s, content, func(int64) { published++ })
	first, status, err := service.UploadMaterial(ctx, id, "upload", uploadInput())
	if err != nil || status != 201 || first.WorkspaceRevision != 4 || first.TaskRef.Revision != 2 {
		t.Fatal(first, status, err)
	}
	detail, err := s.LoadTaskDetails(ctx, id)
	if err != nil || len(detail.Task.Materials) != 2 || detail.Task.Materials[0].MaterialID != nil || detail.Task.Timeline[0].Kind != "task.created" || len(detail.Task.Timeline) != 1 || *detail.Task.Materials[1].MaterialID != first.MaterialID {
		t.Fatal(detail, err)
	}
	query := queries.NewWithMaterials(s, content)
	read, err := query.ReadMaterial(ctx, first.MaterialID)
	if err != nil || read.WorkspaceRevision != 4 || read.Material.Text == nil || *read.Material.Text != uploadInput().Text {
		t.Fatal(read, err)
	}
	for _, sql := range []string{`UPDATE task_materials SET label='changed'`, `DELETE FROM task_materials`} {
		if _, err = s.writer.Exec(sql); err == nil {
			t.Fatal("registry mutable")
		}
	}
	var digest string
	if err = s.writer.QueryRow(`SELECT content_sha256 FROM task_materials`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(filepath.Dir(path), "materials", digest)); err != nil {
		t.Fatal(err)
	}
	missing, err := query.ReadMaterial(ctx, first.MaterialID)
	if err != nil || missing.Material.Text != nil || *missing.Material.UnavailableReason != "missing" {
		t.Fatal(missing, err)
	}
	before := creationFacts(t, s)
	replay, _, err := service.UploadMaterial(ctx, id, "upload", uploadInput())
	if err != nil || replay != first || before != creationFacts(t, s) || published != 1 {
		t.Fatal("replay touched filesystem/state")
	}
	changed := uploadInput()
	changed.Text += "changed"
	_, _, err = service.UploadMaterial(ctx, id, "upload", changed)
	requireCode(t, err, "idempotency_conflict")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	replay, _, err = materials.New(s, content, nil).UploadMaterial(ctx, id, "upload", uploadInput())
	if err != nil || replay != first || before != creationFacts(t, s) {
		t.Fatal("restart replay drift")
	}
	// Original create receipt and task creation associations stay byte-for-byte intact.
	if beforeCreation == before {
		t.Fatal("upload made no saved change")
	}
	var materialsJSON string
	if err = s.writer.QueryRow(`SELECT materials_json FROM task_creation WHERE task_id=?`, id).Scan(&materialsJSON); err != nil {
		t.Fatal(err)
	}
	if materialsJSON == "[]" {
		t.Fatal("original association was rewritten")
	}
}
func TestMaterialRollbackOrphansAndConcurrentKeys(t *testing.T) {
	s, c, _, id := materialFixture(t)
	ctx := context.Background()
	published := atomic.Int32{}
	service := materials.New(s, c, func(int64) { published.Add(1) })
	before := creationFacts(t, s)
	s.beforeCommit = func() error { return errors.New("PRIVATE") }
	_, _, err := service.UploadMaterial(ctx, id, "fault", uploadInput())
	requireCode(t, err, "internal_error")
	if creationFacts(t, s) != before || published.Load() != 0 {
		t.Fatal("failed upload published SQL")
	}
	var count int
	_ = s.writer.QueryRow(`SELECT count(*) FROM task_materials`).Scan(&count)
	if count != 0 {
		t.Fatal("orphan gained authority")
	}
	s.beforeCommit = nil
	var group sync.WaitGroup
	var successes atomic.Int32
	var first materials.Result
	var lock sync.Mutex
	for range 12 {
		group.Go(func() {
			r, _, err := service.UploadMaterial(ctx, id, "same", uploadInput())
			if err != nil {
				t.Error(err)
				return
			}
			lock.Lock()
			if first.MaterialID == "" {
				first = r
			} else if first != r {
				t.Error("replay differed")
			}
			lock.Unlock()
			successes.Add(1)
		})
	}
	group.Wait()
	if successes.Load() != 12 || published.Load() != 1 {
		t.Fatal("concurrent exact replay duplicated")
	}
	stale := uploadInput()
	_, _, err = service.UploadMaterial(ctx, id, "different", stale)
	requireCode(t, err, "revision_conflict")
	for range 12 {
		group.Go(func() {
			in := uploadInput()
			in.ExpectedTaskRevision = 2
			r, _, err := service.UploadMaterial(ctx, id, "different-"+queriesID(), in)
			if err == nil {
				_ = r
				successes.Add(1)
			} else {
				requireCode(t, err, "revision_conflict")
			}
		})
	}
	group.Wait()
	if successes.Load() != 13 || published.Load() != 2 {
		t.Fatal("stale commands both won")
	}
}

var materialSequence atomic.Int32

func queriesID() string { return string(rune('a' + materialSequence.Add(1))) }
func TestMaterialCapacityAndPassiveMissingAssociation(t *testing.T) {
	s, c, _, id := materialFixture(t)
	service := materials.New(s, c, nil)
	ctx := context.Background()
	for revision := int64(1); revision <= 31; revision++ {
		in := uploadInput()
		in.ExpectedTaskRevision = revision
		if _, _, err := service.UploadMaterial(ctx, id, "upload-"+strconv.FormatInt(revision, 10), in); err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.LoadTaskDetails(ctx, id)
	if err != nil || len(before.Task.Materials) != 32 {
		t.Fatal(err)
	}
	in := uploadInput()
	in.ExpectedTaskRevision = 32
	_, _, err = service.UploadMaterial(ctx, id, "capacity", in)
	requireCode(t, err, "unsupported_operation")
	after, _ := s.LoadTaskDetails(ctx, id)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("capacity changed saved task")
	}
}
