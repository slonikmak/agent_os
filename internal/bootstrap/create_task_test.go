package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/wailsread"
	v1 "personaldesk/internal/contracts/v1"
)

func TestActualHTTPCreateNativeSavedViewsInvalidationAndRestart(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	credentials, err := loadCredentials(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	revisions := []int64{}
	stop := w.SubscribeChanges(func(revision int64) { mu.Lock(); defer mu.Unlock(); revisions = append(revisions, revision) })
	defer stop()
	submit := func(path, role, key, body string) ([]byte, int) {
		t.Helper()
		r, _ := http.NewRequest("POST", discovery(t, root).Endpoint+path, strings.NewReader(body))
		token := credentials.Worker
		if role == "coordinator" {
			token = credentials.Coordinator
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return data, response.StatusCode
	}
	data, status := submit("/api/v1/inbox/messages", "worker", "receive", `{"source":"manual","scope":"synthetic","messageId":"create","kind":"external_signal","text":"Original signal"}`)
	if status != 201 {
		t.Fatal(status, string(data))
	}
	var received v1.ReceiveResult
	_ = json.Unmarshal(data, &received)
	path := "/api/v1/inbox/messages/" + received.Message.ID + "/resolutions"
	body := `{"expectedMessageRevision":1,"action":"createTask","reason":"Separate coordinator decision","task":{"kind":"request","title":"Saved native card"}}`
	data, status = submit(path, "coordinator", "create", body)
	if status != 201 {
		t.Fatal(status, string(data))
	}
	var created v1.CreateTaskResult
	_ = json.Unmarshal(data, &created)
	taskNative := wailsread.New(w, diagnostics.New(), "").GetTaskDetails(context.Background(), created.Task.ID)
	taskJSON, _ := json.Marshal(taskNative.Data)
	expectedTaskJSON, _ := json.Marshal(v1.TaskDetails{WorkspaceRevision: 3, Task: created.Task})
	if taskNative.Error != nil || string(taskJSON) != string(expectedTaskJSON) {
		t.Fatal("native task differs from created facts", taskNative.Error)
	}
	for range 3 {
		replay, status := submit(path, "coordinator", "create", body)
		if status != 201 || string(replay) != string(data) {
			t.Fatal("HTTP replay drift")
		}
	}
	native := wailsread.New(w, diagnostics.New(), "").GetInboxMessage(context.Background(), received.Message.ID)
	if native.Error != nil {
		t.Fatal(native.Error)
	}
	nativeJSON, _ := json.Marshal(native.Data)
	wireJSON, _ := json.Marshal(v1.InboxDetails{WorkspaceRevision: 3, Message: created.Message})
	if string(nativeJSON) != string(wireJSON) {
		t.Fatal("native/HTTP resolution differs")
	}
	snapshot, err := w.GetWorkspaceSnapshot(context.Background())
	if err != nil || snapshot.WorkspaceRevision != 3 || snapshot.Counts.Tasks != 1 || snapshot.Counts.UnprocessedInbox != 0 || snapshot.Tasks[0].ID != created.Task.ID || snapshot.Inbox[0].Status != "applied" {
		t.Fatal("native card/count drift", err)
	}
	mu.Lock()
	if !reflect.DeepEqual(revisions, []int64{2, 3}) {
		t.Fatal("replay publication", revisions)
	}
	mu.Unlock()
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = w.GetTaskDetails(context.Background(), created.Task.ID); err == nil {
		t.Fatal("closed workspace allowed task read")
	}
	again := start(t, root)
	taskAfter := wailsread.New(again, diagnostics.New(), "").GetTaskDetails(context.Background(), created.Task.ID)
	if !reflect.DeepEqual(taskNative, taskAfter) {
		t.Fatal("task details changed across restart")
	}
	after := wailsread.New(again, diagnostics.New(), "").GetInboxMessage(context.Background(), received.Message.ID)
	if !reflect.DeepEqual(after.Data, native.Data) {
		t.Fatal("native restart lost resolution")
	}
	replay, status := submit(path, "coordinator", "create", body)
	if status != 201 || string(replay) != string(data) {
		t.Fatal("restart replay drift")
	}
}
