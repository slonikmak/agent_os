package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/wailsread"
	v1 "personaldesk/internal/contracts/v1"
)

func TestDiagnosticRotationRepairsPartialRecordsAndRejectsAliases(t *testing.T) {
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	sink, interrupted, err := openDiagnostics(root, diagnostics.ID())
	if err != nil || interrupted {
		t.Fatal(err)
	}
	sink.limit = 900
	b := diagnostics.New()
	b.Attach(sink, false)
	for range 60 {
		b.Record("api.operation", "workspace.read", diagnostics.ID(), "")
	}
	b.Close()
	entries, err := os.ReadDir(filepath.Join(root, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("inventory=%d", len(entries))
	}
	for _, entry := range entries {
		path := filepath.Join(root, "logs", entry.Name())
		info, _ := entry.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal("public diagnostic file")
		}
		if entry.Name() == "session.json" {
			continue
		}
		data, _ := os.ReadFile(path)
		if len(data) > 900 {
			t.Fatal("rotation size bound")
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'}) {
			if _, err := v1.Validate("diagnostic-event", bytes.NewReader(line), v1.MaxDiagnosticRecordBytes); err != nil {
				t.Fatal(err)
			}
		}
	}
	active := filepath.Join(root, "logs", logName(0))
	f, _ := os.OpenFile(active, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(`{"SECRET":"/private`)
	f.Close()
	again, interrupted, err := openDiagnostics(root, diagnostics.ID())
	if err != nil || interrupted {
		t.Fatal(err)
	}
	again.Close()
	data, _ := os.ReadFile(active)
	if bytes.Contains(data, []byte("SECRET")) || data[len(data)-1] != '\n' {
		t.Fatal("partial record retained")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err = os.WriteFile(target, []byte("KEEP"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(active); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, active); err != nil {
		t.Fatal(err)
	}
	if sink, _, err = openDiagnostics(root, diagnostics.ID()); err == nil {
		sink.Close()
		t.Fatal("followed log alias")
	}
	data, _ = os.ReadFile(target)
	if string(data) != "KEEP" {
		t.Fatal("target changed")
	}
}
func TestInterruptedMarkerAndPostCloseCallbacksNeverWriteSharedFiles(t *testing.T) {
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	first, _, err := openDiagnostics(root, diagnostics.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err = first.CloseFiles(); err != nil {
		t.Fatal(err)
	} // process interruption, no clean marker
	b := diagnostics.New()
	attachDiagnostics(root, b)
	if !b.Status().InterruptedPriorSession {
		t.Fatal("interruption not reported")
	}
	b.Record("app.start", "startup", "", "")
	b.Close()
	before, _ := os.ReadFile(filepath.Join(root, "logs", logName(0)))
	markerBefore, _ := os.ReadFile(filepath.Join(root, "logs", "session.json"))
	for range 200 {
		b.Fault("bootstrap", "framework", "", "panic")
	}
	after, _ := os.ReadFile(filepath.Join(root, "logs", logName(0)))
	markerAfter, _ := os.ReadFile(filepath.Join(root, "logs", "session.json"))
	if !bytes.Equal(before, after) || !bytes.Equal(markerBefore, markerAfter) || len(b.Snapshot()) != 128 {
		t.Fatal("late callback wrote shared state")
	}
	final, interrupted, err := openDiagnostics(root, diagnostics.ID())
	if err != nil || interrupted {
		t.Fatal("clean close marked interrupted")
	}
	final.Close()
}
func TestFailedNativeStartupRetainsOnlyDiagnosticOwnership(t *testing.T) {
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace.sqlite"), []byte("SYNTHETIC SECRET INVALID DATABASE"), 0600); err != nil {
		t.Fatal(err)
	}
	diag := diagnostics.New()
	session, err := StartSession(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diag)
	var startup *StartupError
	if session == nil || !errors.As(err, &startup) || startup.ErrorID == "" || startup.RequestID == "" {
		t.Fatal("no owned failure session")
	}
	defer session.Close()
	if !diag.Status().SharedAvailable {
		t.Fatal("diagnostics unavailable")
	}
	if lock, err := acquireLock(filepath.Join(root, "workspace.lock")); err == nil {
		releaseLock(lock)
		t.Fatal("failed shell released ownership")
	}
	if _, err := os.Stat(filepath.Join(root, "discovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed API advertised")
	}
	result := wailsread.NewStartup(nil, diag, startup.Code, startup.ErrorID).GetWorkspaceSnapshot(context.Background())
	if result.Error == nil || result.Error.ErrorID == nil || *result.Error.ErrorID != startup.ErrorID {
		t.Fatal("startup correlation lost")
	}
	data, _ := json.Marshal(diag.Snapshot())
	if strings.Contains(string(data), "SECRET") || strings.Contains(string(data), root) {
		t.Fatal("startup content leaked")
	}
	found := false
	for _, event := range diag.Snapshot() {
		if event.ErrorID == startup.ErrorID {
			found = true
		}
	}
	if !found {
		t.Fatal("startup ID not locatable")
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
	if err != nil {
		t.Fatal("lock not released after drain")
	}
	releaseLock(lock)
}
func TestUnsafeDiagnosticDirectoryDegradesWithoutBlockingReadiness(t *testing.T) {
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(root, "logs")); err != nil {
		t.Fatal(err)
	}
	diag := diagnostics.New()
	w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diag)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if !diag.Status().Degraded || diag.Status().SharedAvailable {
		t.Fatal("unsafe sink accepted")
	}
	if _, err = w.GetWorkspaceSnapshot(context.Background()); err != nil {
		t.Fatal("diagnostics changed readiness")
	}
	entries, _ := os.ReadDir(other)
	if len(entries) != 0 {
		t.Fatal("escaped private directory")
	}
}

type deniedDiagnosticSink struct{ panics bool }

func (s deniedDiagnosticSink) Append([]byte) error {
	if s.panics {
		panic("SYNTHETIC_SECRET BUSINESS /private")
	}
	return os.ErrPermission
}
func (s deniedDiagnosticSink) Close() error {
	if s.panics {
		panic("SYNTHETIC_SECRET /private")
	}
	return os.ErrPermission
}
func TestCommittedHTTPResultsReceiptsAndStateSurviveDiagnosticFailure(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprint(panics), func(t *testing.T) {
			root := newRoot(t)
			w := start(t, root)
			w.diag.Attach(deniedDiagnosticSink{panics}, false)
			credentials, err := loadCredentials(filepath.Join(root, "credentials.json"))
			if err != nil {
				t.Fatal(err)
			}
			// A notification panic after commit also passes through the denied sink.
			stop := w.SubscribeChanges(func(int64) { panic("SYNTHETIC_SECRET BUSINESS /private") })
			defer stop()
			post := func(path, key, body string) []byte {
				r, _ := http.NewRequest("POST", discovery(t, root).Endpoint+path, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+credentials.Coordinator)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", key)
				response, err := http.DefaultClient.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, _ := io.ReadAll(response.Body)
				if response.StatusCode != 201 {
					t.Fatalf("committed response changed: %d %s", response.StatusCode, data)
				}
				return data
			}
			receiveBody := `{"source":"manual","scope":"SYNTHETIC_SECRET","messageId":"one","kind":"user_request","text":"BUSINESS /private"}`
			received := post("/api/v1/inbox/messages", "receive", receiveBody)
			var message v1.ReceiveResult
			_ = json.Unmarshal(received, &message)
			createPath := "/api/v1/inbox/messages/" + message.Message.ID + "/resolutions"
			createBody := `{"expectedMessageRevision":1,"action":"createTask","reason":"BUSINESS /private","task":{"kind":"request","title":"SYNTHETIC_SECRET"}}`
			created := post(createPath, "create", createBody)
			var task v1.CreateTaskResult
			_ = json.Unmarshal(created, &task)
			uploadPath := "/api/v1/tasks/" + task.Task.ID + "/materials"
			uploadBody := `{"expectedTaskRevision":1,"mediaType":"text/plain","text":"SYNTHETIC_SECRET BUSINESS /private"}`
			uploaded := post(uploadPath, "upload", uploadBody)
			before, err := w.GetWorkspaceSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "workspace.sqlite"))+"?mode=ro")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			receipts := func() []string {
				rows, err := db.Query("SELECT response_json FROM mutation_receipts ORDER BY client_id,operation,idempotency_key")
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				var out []string
				for rows.Next() {
					var value string
					if err := rows.Scan(&value); err != nil {
						t.Fatal(err)
					}
					out = append(out, value)
				}
				return out
			}
			facts := func() []byte {
				all := []any{}
				for _, table := range []string{"workspace_meta", "workspace_counters", "inbox_messages", "inbox_payloads", "tasks", "task_creation", "task_events", "inbox_resolutions", "mutation_receipts", "task_materials"} {
					rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1,2")
					if err != nil {
						t.Fatal(err)
					}
					columns, _ := rows.Columns()
					saved := [][]any{}
					for rows.Next() {
						values := make([]any, len(columns))
						dest := make([]any, len(columns))
						for i := range values {
							dest[i] = &values[i]
						}
						if err = rows.Scan(dest...); err != nil {
							t.Fatal(err)
						}
						saved = append(saved, values)
					}
					if err = rows.Err(); err != nil {
						t.Fatal(err)
					}
					rows.Close()
					all = append(all, saved)
				}
				data, _ := json.Marshal(all)
				return data
			}
			originalFacts := facts()
			originals := receipts()
			for range 3 {
				if !bytes.Equal(received, post("/api/v1/inbox/messages", "receive", receiveBody)) || !bytes.Equal(created, post(createPath, "create", createBody)) || !bytes.Equal(uploaded, post(uploadPath, "upload", uploadBody)) {
					t.Fatal("exact replay result changed")
				}
			}
			after, err := w.GetWorkspaceSnapshot(context.Background())
			if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(originals, receipts()) || !bytes.Equal(originalFacts, facts()) {
				t.Fatal("receipt/state changed during replay")
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			if !w.diag.Status().Degraded {
				t.Fatal("sink failure not visible")
			}
			encoded, _ := json.Marshal(w.diag.Snapshot())
			for _, marker := range []string{"SYNTHETIC_SECRET", "BUSINESS", "/private", root} {
				if bytes.Contains(encoded, []byte(marker)) {
					t.Fatal("diagnostic privacy failed")
				}
			}
		})
	}
}

func TestDiagnosticDirectoryReplacementCannotRedirectOwnedWrites(t *testing.T) {
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	sink, _, err := openDiagnostics(root, diagnostics.ID())
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "logs")
	moved := filepath.Join(root, "retained-logs")
	if err = os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err = os.Symlink(target, original); err != nil {
		t.Fatal(err)
	}
	diag := diagnostics.New()
	diag.Attach(sink, false)
	diag.Record("app.start", "startup", "", "")
	diag.Close()
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("writes redirected through replaced directory")
	}
	data, _ := os.ReadFile(filepath.Join(moved, logName(0)))
	if !bytes.Contains(data, []byte(`"app.start"`)) {
		t.Fatal("pinned directory lost")
	}
	if sink, _, err = openDiagnostics(root, diagnostics.ID()); err == nil {
		sink.Close()
		t.Fatal("accepted replaced log path")
	}
}

func TestUnsafeInvariantHaltGatesDomainUntilRestartAndKeepsDiagnosticOwnership(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	w.Halt()
	if !w.diag.Status().RestartRequired || !w.diag.Status().SharedAvailable {
		t.Fatal("halt not reported")
	}
	if _, err := w.GetWorkspaceSnapshot(context.Background()); err == nil {
		t.Fatal("unsafe work continued")
	}
	if lock, err := acquireLock(filepath.Join(root, "workspace.lock")); err == nil {
		releaseLock(lock)
		t.Fatal("halt released diagnostic ownership")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
