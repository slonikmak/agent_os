package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/wailsread"
	v1 "personaldesk/internal/contracts/v1"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestActualHTTPReceiveNativeDetailsInvalidationAndRestart(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	credentials, err := loadCredentials(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := discovery(t, root).Endpoint
	var events atomic.Int32
	unsubscribe := w.SubscribeChanges(func(revision int64) {
		if revision != 2 {
			t.Errorf("unexpected revision %d", revision)
		}
		events.Add(1)
	})
	defer unsubscribe()
	broken := w.SubscribeChanges(func(int64) { panic("safe callback failure") })
	defer broken()
	body := `{"source":"manual","scope":"synthetic","messageId":"first","kind":"user_request","sender":"Claimed sender","text":"Original synthetic body"}`
	submit := func(key string) (v1.ReceiveResult, int) {
		t.Helper()
		r, _ := http.NewRequest("POST", endpoint+"/api/v1/inbox/messages", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+credentials.Worker)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		var result v1.ReceiveResult
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result, response.StatusCode
	}
	first, status := submit("first")
	if status != 201 {
		t.Fatal(status)
	}
	native := wailsread.New(w, diagnostics.New(), "").GetInboxMessage(context.Background(), first.Message.ID)
	if native.Error != nil {
		t.Fatal(native.Error)
	}
	nativeJSON, _ := json.Marshal(native.Data)
	wireJSON, _ := json.Marshal(first)
	if string(nativeJSON) != string(wireJSON) {
		t.Fatal("HTTP/native details differ")
	}
	replay, status := submit("first")
	if status != 201 || !reflect.DeepEqual(replay, first) {
		t.Fatal("lost response replay changed")
	}
	duplicate, status := submit("second")
	if status != 200 || !reflect.DeepEqual(duplicate, first) {
		t.Fatal("identity redelivery changed")
	}
	if events.Load() != 1 {
		t.Fatal("duplicate invalidation", events.Load())
	}
	for range 4 {
		if detail := wailsread.New(w, diagnostics.New(), "").GetInboxMessage(context.Background(), first.Message.ID); !reflect.DeepEqual(detail.Data, native.Data) {
			t.Fatal("passive detail changed canonical state")
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = w.GetInboxMessage(context.Background(), first.Message.ID); err == nil {
		t.Fatal("closed workspace allowed query")
	}
	again := start(t, root)
	after := wailsread.New(again, diagnostics.New(), "").GetInboxMessage(context.Background(), first.Message.ID)
	if !reflect.DeepEqual(after.Data, native.Data) {
		t.Fatal("restart lost original message")
	}
	snapshot, err := again.GetWorkspaceSnapshot(context.Background())
	if err != nil || snapshot.Counts.Tasks != 0 || snapshot.Counts.Inbox != 1 || snapshot.WorkspaceRevision != 2 {
		t.Fatal("receive created task or extra revision", err)
	}
}
func TestSubscriptionStopWaitsForCallbackAndClosePreventsLatePublications(t *testing.T) {
	w := start(t, newRoot(t))
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	stop := w.SubscribeChanges(func(int64) { calls.Add(1); close(entered); <-release })
	published := make(chan struct{})
	go func() { w.publishRevision(2); close(published) }()
	<-entered
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("unsubscribe returned while callback active")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	<-stopped
	<-published
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() { defer group.Done(); w.publishRevision(3) }()
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatal("callback after unsubscribe")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stop = w.SubscribeChanges(func(int64) { calls.Add(1) })
	w.publishRevision(4)
	stop()
	if calls.Load() != 1 {
		t.Fatal("callback after lifetime ended")
	}
}
