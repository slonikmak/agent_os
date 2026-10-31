package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"personaldesk/internal/adapters/sqlite"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/materials"
	v1 "personaldesk/internal/contracts/v1"
)

// The actual workflow services recover a panic inside a real SQLite transaction;
// no fake Receiver/TaskCreator/Uploader substitutes for their recovery boundary.
type commandFaultWork struct {
	store *sqlite.Store
	kind  string
	calls int
}

func (w *commandFaultWork) fault() error {
	w.calls++
	switch w.kind {
	case "panic":
		panic("PRIVATE PANIC SQL /private")
	case "rollback":
		return errors.New("PRIVATE SQL /private")
	case "expected":
		return failure.New("unsupported_operation")
	case "cancelled":
		return context.Canceled
	}
	return nil
}
func (w *commandFaultWork) Within(ctx context.Context, fn func(intake.ReceiveTx) error) error {
	return w.store.Within(ctx, func(tx intake.ReceiveTx) error {
		if err := w.fault(); err != nil {
			return err
		}
		return fn(tx)
	})
}
func (w *commandFaultWork) WithinCreateTask(ctx context.Context, fn func(intake.CreateTaskTx) error) error {
	return w.store.WithinCreateTask(ctx, func(tx intake.CreateTaskTx) error {
		if err := w.fault(); err != nil {
			return err
		}
		return fn(tx)
	})
}
func (w *commandFaultWork) WithinMaterials(ctx context.Context, fn func(materials.Tx) error) error {
	return w.store.WithinMaterials(ctx, func(tx materials.Tx) error {
		if err := w.fault(); err != nil {
			return err
		}
		return fn(tx)
	})
}

func TestActualCommandRecoveryClassifiesPanicsAndGatesUnsafeMutations(t *testing.T) {
	for _, operation := range []string{"receive", "create", "upload"} {
		for _, kind := range []string{"panic", "rollback", "expected", "cancelled"} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				h, store, diag := receiveHandler(t)
				work := &commandFaultWork{store: store, kind: kind}
				h.receiver = intake.NewReceiver(work, nil)
				h.creator = intake.NewTaskCreator(work, nil)
				h.uploader = materials.New(work, nil, nil)
				path, body, catalog := "/api/v1/inbox/messages", receiveBody, "inbox.receive"
				if operation == "create" {
					path = "/api/v1/inbox/messages/I-one/resolutions"
					body = createBody
					catalog = "inbox.resolve.createTask"
				}
				if operation == "upload" {
					path = "/api/v1/tasks/T-one/materials"
					body = `{"expectedTaskRevision":1,"mediaType":"text/plain","text":"PRIVATE business"}`
					catalog = "framework"
				}
				before, err := store.LoadWorkspace(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				response := command(h, "coordinator", "fault", body, func(r *http.Request) { r.URL.Path = path })
				want := 500
				if kind == "expected" {
					want = 400
				}
				if kind == "cancelled" {
					want = 503
				}
				if response.Code != want || diag.Status().RestartRequired != (kind == "panic") {
					t.Fatalf("wrong recovery outcome %d restart=%v", response.Code, diag.Status().RestartRequired)
				}
				var envelope v1.ErrorEnvelope
				if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				faults := 0
				for _, event := range diag.Snapshot() {
					if event.Code == "query.fault" {
						faults++
						class := "internal"
						if kind == "panic" {
							class = "panic"
						}
						if event.ErrorClass != class || event.Operation != catalog || event.ErrorID != envelope.Error.ErrorID || event.RequestID != envelope.Error.RequestID {
							t.Fatal("wrong recovered fault correlation", event)
						}
					}
				}
				expectedFaults := 1
				if kind == "expected" || kind == "cancelled" {
					expectedFaults = 0
				}
				if faults != expectedFaults {
					t.Fatal("wrong fault count", faults)
				}
				again := command(h, "coordinator", "fault", body, func(r *http.Request) { r.URL.Path = path })
				if kind == "panic" {
					if again.Code != 503 || work.calls != 1 {
						t.Fatal("mutation continued after recovered command panic")
					}
				} else if work.calls != 2 || again.Code != want {
					t.Fatal("ordinary rollback/outcome incorrectly gated")
				}
				after, err := store.LoadWorkspace(context.Background())
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("failed command changed saved state")
				}
				events, _ := json.Marshal(diag.Snapshot())
				for _, marker := range []string{"PRIVATE", "SQL", "/private"} {
					if strings.Contains(response.Body.String()+string(events), marker) {
						t.Fatal("recovered value leaked")
					}
				}
			})
		}
	}
}

type deniedCommandDiagnosticSink struct{}

func (deniedCommandDiagnosticSink) Append([]byte) error { return errors.New("PRIVATE sink") }
func (deniedCommandDiagnosticSink) Close() error        { return errors.New("PRIVATE sink") }

type commandContent struct{}

func (commandContent) Put(context.Context, string, []byte) error { return nil }
func TestPostcommitNotificationsAndDiagnosticsDoNotBecomeCommandPanics(t *testing.T) {
	h, store, diag := receiveHandler(t)
	diag.Attach(deniedCommandDiagnosticSink{}, false)
	defer diag.Close()
	notify := func(int64) { panic("PRIVATE postcommit") }
	h.receiver = intake.NewReceiver(store, notify)
	h.creator = intake.NewTaskCreator(store, notify)
	h.uploader = materials.New(store, commandContent{}, notify)
	receive := command(h, "coordinator", "receive", eligibleBody, nil)
	if receive.Code != 201 {
		t.Fatal(receive.Body.String())
	}
	var received v1.ReceiveResult
	_ = json.Unmarshal(receive.Body.Bytes(), &received)
	createPath := "/api/v1/inbox/messages/" + received.Message.ID + "/resolutions"
	create := command(h, "coordinator", "create", createBody, func(r *http.Request) { r.URL.Path = createPath })
	if create.Code != 201 {
		t.Fatal(create.Body.String())
	}
	var created v1.CreateTaskResult
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	uploadPath := "/api/v1/tasks/" + created.Task.ID + "/materials"
	uploadBody := `{"expectedTaskRevision":1,"mediaType":"text/plain","text":"PRIVATE business"}`
	upload := command(h, "coordinator", "upload", uploadBody, func(r *http.Request) { r.URL.Path = uploadPath })
	if upload.Code != 201 {
		t.Fatal(upload.Body.String())
	}
	for _, saved := range []struct{ path, key, body, response string }{{"/api/v1/inbox/messages", "receive", eligibleBody, receive.Body.String()}, {createPath, "create", createBody, create.Body.String()}, {uploadPath, "upload", uploadBody, upload.Body.String()}} {
		replay := command(h, "coordinator", saved.key, saved.body, func(r *http.Request) { r.URL.Path = saved.path })
		if replay.Code != 201 || replay.Body.String() != saved.response {
			t.Fatal("postcommit/diagnostic failure changed exact replay")
		}
	}
	diag.Close()
	if !diag.Status().Degraded || diag.Status().RestartRequired {
		t.Fatal("maintenance failure changed command safety gate")
	}
}
