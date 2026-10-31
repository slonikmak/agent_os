package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/sqlite"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
)

const receiveBody = `{"source":"manual","scope":"synthetic","messageId":"first","kind":"user_request","text":"PRIVATE_BODY <script> & original\nline","sourceUrl":"https://example.invalid/source","taskRef":{"taskId":"T-proposed","revision":4},"artifacts":[{"kind":"file","coordinate":"/PRIVATE_COORDINATE","label":null}]}`

func receiveHandler(t *testing.T) (*Handler, *sqlite.Store, *diagnostics.Buffer) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	diag := diagnostics.New()
	return New(queries.New(store), testAuthenticator(t), "127.0.0.1:8765", diag, intake.NewReceiver(store, nil)), store, diag
}
func command(h http.Handler, role, key, body string, change func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/inbox/messages", strings.NewReader(body))
	r.Host = "127.0.0.1:8765"
	r.Header.Set("Authorization", "Bearer "+testToken(role))
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("Content-Type", "application/json")
	if change != nil {
		change(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestReceiveHTTPContractsReplayNormalizedPresenceAndDetail(t *testing.T) {
	h, store, _ := receiveHandler(t)
	first := command(h, "worker", "first", receiveBody, nil)
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	if _, err := v1.Validate("receive-result", bytes.NewReader(first.Body.Bytes()), v1.MaxResponseBytes); err != nil {
		t.Fatal("receive schema", err)
	}
	var result v1.ReceiveResult
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	normalized := strings.TrimSuffix(receiveBody, "}") + `,"sender":null,"occurredAt":null}`
	replay := command(h, "worker", "first", normalized, nil)
	if replay.Code != 201 || replay.Body.String() != first.Body.String() || replay.Header().Get("X-Request-ID") == first.Header().Get("X-Request-ID") {
		t.Fatal("normalized replay drift", replay.Body.String())
	}
	duplicate := command(h, "coordinator", "second", normalized, nil)
	if duplicate.Code != 200 || duplicate.Body.String() != first.Body.String() {
		t.Fatal("identity duplicate/provenance drift", duplicate.Body.String())
	}
	detail := request(h, "/api/v1/inbox/messages/"+result.Message.ID, "127.0.0.1:8765", "reader", "", "")
	if detail.Code != 200 || detail.Body.String() != first.Body.String() {
		t.Fatal("detail drift", detail.Body.String())
	}
	metadata := request(h, "/api/v1/workspace", "127.0.0.1:8765", "reader", "", "")
	if metadata.Code != 200 || strings.Contains(metadata.Body.String(), "PRIVATE_BODY") || strings.Contains(metadata.Body.String(), "PRIVATE_COORDINATE") {
		t.Fatal("body in metadata")
	}
	discovery := request(h, "/api/v1", "127.0.0.1:8765", "reader", "", "")
	var description v1.Discovery
	_ = json.Unmarshal(discovery.Body.Bytes(), &description)
	if !reflect.DeepEqual(description.Capabilities, []string{"workspace.read", "tasks.read", "inbox.read", "inbox.receive"}) {
		t.Fatal(description.Capabilities)
	}
	for _, test := range []struct{ key, code string }{{"first", "idempotency_conflict"}, {"third", "message_identity_conflict"}} {
		w := command(h, "worker", test.key, strings.Replace(receiveBody, "PRIVATE_BODY", "CHANGED", 1), nil)
		if w.Code != 409 || !strings.Contains(w.Body.String(), test.code) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	snapshot, err := store.LoadWorkspace(context.Background())
	if err != nil || snapshot.WorkspaceRevision != 2 || snapshot.Counts.Inbox != 1 || snapshot.Counts.Tasks != 0 {
		t.Fatal("HTTP changed domain unexpectedly", err)
	}
	missing := request(h, "/api/v1/inbox/messages/I-missing", "127.0.0.1:8765", "reader", "", "")
	if missing.Code != 404 {
		t.Fatal(missing.Code)
	}
}
func TestReceiveRejectsUntrustedInputsBeforeEffectsAndNeverLogsContent(t *testing.T) {
	h, store, diag := receiveHandler(t)
	tests := []struct {
		name, role, key, body, code string
		status                      int
		change                      func(*http.Request)
	}{
		{name: "reader", role: "reader", key: "key", body: receiveBody, status: 403, code: "forbidden"},
		{name: "bad-auth", role: "worker", key: "key", body: receiveBody, status: 401, code: "unauthorized", change: func(r *http.Request) { r.Header.Set("Authorization", "Bearer PRIVATE_TOKEN") }},
		{name: "host", role: "worker", key: "key", body: receiveBody, status: 403, code: "forbidden", change: func(r *http.Request) { r.Host = "localhost:8765" }},
		{name: "origin", role: "worker", key: "key", body: receiveBody, status: 403, code: "forbidden", change: func(r *http.Request) { r.Header.Set("Origin", "") }},
		{name: "key", role: "worker", key: "bad key", body: receiveBody, status: 400, code: "invalid_request"},
		{name: "missing-key", role: "worker", key: "key", body: receiveBody, status: 400, code: "invalid_request", change: func(r *http.Request) { r.Header.Del("Idempotency-Key") }},
		{name: "duplicate-key", role: "worker", key: "key", body: receiveBody, status: 400, code: "invalid_request", change: func(r *http.Request) { r.Header.Add("Idempotency-Key", "key") }},
		{name: "media", role: "worker", key: "key", body: receiveBody, status: 415, code: "unsupported_media_type", change: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{name: "version", role: "worker", key: "key", body: receiveBody, status: 400, code: "unsupported_api_version", change: func(r *http.Request) { r.Header.Set("X-API-Version", "v2") }},
		{name: "query", role: "worker", key: "key", body: receiveBody, status: 400, code: "invalid_request", change: func(r *http.Request) { r.URL.RawQuery = "token=PRIVATE_TOKEN" }},
		{name: "unknown-field", role: "worker", key: "key", body: strings.TrimSuffix(receiveBody, "}") + `,"submittedBy":{"clientId":"spoof"}}`, status: 400, code: "invalid_request"},
		{name: "duplicate-json", role: "worker", key: "key", body: strings.TrimSuffix(receiveBody, "}") + `,"text":"PRIVATE_OTHER"}`, status: 400, code: "invalid_request"},
		{name: "null-artifacts", role: "worker", key: "key", body: `{"source":"manual","scope":"test","messageId":"one","kind":"user_request","text":"PRIVATE_BODY","artifacts":null}`, status: 400, code: "invalid_request"},
		{name: "too-large", role: "worker", key: "key", body: strings.Repeat("x", v1.MaxBodyBytes+1), status: 413, code: "payload_too_large"},
		{name: "invalid-utf8", role: "worker", key: "key", body: receiveBody + string([]byte{255}), status: 400, code: "invalid_request"},
		{name: "invalid-time", role: "worker", key: "key", body: strings.TrimSuffix(receiveBody, "}") + `,"occurredAt":"2026-10-29T00:00:00+03:00"}`, status: 400, code: "invalid_request"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := command(h, test.role, test.key, test.body, test.change)
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) {
				t.Fatal(w.Code, w.Body.String())
			}
			if _, err := v1.Validate("error", bytes.NewReader(w.Body.Bytes()), v1.MaxResponseBytes); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(w.Body.String(), "PRIVATE_") {
				t.Fatal("private input leaked")
			}
		})
	}
	snapshot, err := store.LoadWorkspace(context.Background())
	if err != nil || snapshot.WorkspaceRevision != 1 || snapshot.Counts.Inbox != 0 {
		t.Fatal("rejected request changed state", err)
	}
	data, _ := json.Marshal(diag.Snapshot())
	if strings.Contains(string(data), "PRIVATE_") {
		t.Fatal("input entered diagnostic records")
	}
	for _, path := range []string{"/api/v1/inbox/messages/I-one/resolutions", "/api/v1/provider/execute", "/api/v1/diagnostics/export"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(receiveBody))
		r.Host = "127.0.0.1:8765"
		r.Header.Set("Authorization", "Bearer "+testToken("coordinator"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("future command enabled", path, w.Code)
		}
	}
}
