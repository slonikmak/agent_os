package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/intake"
	v1 "personaldesk/internal/contracts/v1"
)

const createBody = `{"expectedMessageRevision":1,"action":"createTask","reason":"A new obligation","task":{"kind":"request","title":"Synthetic saved task"}}`
const eligibleBody = `{"source":"manual","scope":"synthetic","messageId":"eligible","kind":"user_request","text":"PRIVATE original body","artifacts":[{"kind":"file","coordinate":"/PRIVATE_COORDINATE","label":null}]}`

var malformedResolutionPaths = []string{
	"/api/v1/inbox/messages/resolutions",
	"/api/v1/inbox/messages//resolutions",
	"/api/v1/inbox/messages/I-one/extra/resolutions",
	"/api/v1/inbox/messages/I-one/resolutions/",
}

func TestCreateHTTPFullResultNormalizedReplayAndSavedReads(t *testing.T) {
	h, store, _ := receiveHandler(t)
	h.creator = intake.NewTaskCreator(store, nil)
	seed := command(h, "worker", "receive", eligibleBody, nil)
	if seed.Code != 201 {
		t.Fatal(seed.Code, seed.Body.String())
	}
	var received v1.ReceiveResult
	_ = json.Unmarshal(seed.Body.Bytes(), &received)
	path := "/api/v1/inbox/messages/" + received.Message.ID + "/resolutions"
	first := command(h, "coordinator", "create", createBody, func(r *http.Request) { r.URL.Path = path })
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	if _, err := v1.Validate("create-task-result", bytes.NewReader(first.Body.Bytes()), v1.MaxResponseBytes); err != nil {
		t.Fatal(err)
	}
	var created v1.CreateTaskResult
	_ = json.Unmarshal(first.Body.Bytes(), &created)
	if created.Message.SubmittedBy.Role != "worker" || created.Message.Resolution.AppliedBy.Role != "coordinator" || created.Task.Provenance.MessageID != received.Message.ID || created.Task.Summary != nil || created.Task.Materials[0].MaterialID != nil {
		t.Fatal("transport changed facts", created)
	}
	normalized := strings.Replace(createBody, `"title":"Synthetic saved task"`, `"title":"Synthetic saved task","summary":null,"nextAction":null,"nextActor":null`, 1)
	replay := command(h, "coordinator", "create", normalized, func(r *http.Request) { r.URL.Path = path })
	if replay.Code != 201 || replay.Body.String() != first.Body.String() || replay.Header().Get("X-Request-ID") == first.Header().Get("X-Request-ID") {
		t.Fatal("normalized original replay drift", replay.Body.String())
	}
	for _, malformed := range malformedResolutionPaths {
		w := command(h, "coordinator", "create", createBody, func(r *http.Request) { r.URL.Path = malformed })
		if w.Code != 404 || !strings.Contains(w.Body.String(), `"code":"not_found"`) {
			t.Fatal("malformed route reached committed-key replay", malformed, w.Code, w.Body.String())
		}
	}
	for _, test := range []struct{ key, body, code string }{{"create", strings.Replace(createBody, "obligation", "changed", 1), "idempotency_conflict"}, {"different", createBody, "terminal_resolution"}} {
		w := command(h, "coordinator", test.key, test.body, func(r *http.Request) { r.URL.Path = path })
		if w.Code != 409 || !strings.Contains(w.Body.String(), test.code) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	details := request(h, "/api/v1/inbox/messages/"+received.Message.ID, "127.0.0.1:8765", "reader", "", "")
	var saved v1.InboxDetails
	_ = json.Unmarshal(details.Body.Bytes(), &saved)
	if details.Code != 200 || !reflect.DeepEqual(saved.Message, created.Message) || saved.WorkspaceRevision != 3 {
		t.Fatal("saved detail differs", details.Body.String())
	}
	discovery := request(h, "/api/v1", "127.0.0.1:8765", "reader", "", "")
	var description v1.Discovery
	_ = json.Unmarshal(discovery.Body.Bytes(), &description)
	if !reflect.DeepEqual(description.Capabilities, []string{"workspace.read", "tasks.read", "inbox.read", "inbox.receive", "inbox.resolve.createTask"}) {
		t.Fatal(description.Capabilities)
	}
	for _, role := range []string{"reader", "worker", "coordinator"} {
		response := request(h, "/api/v1/tasks/"+created.Task.ID, "127.0.0.1:8765", role, "", "")
		var saved v1.TaskDetails
		if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || response.Code != 200 || saved.WorkspaceRevision != 3 || !reflect.DeepEqual(saved.Task, created.Task) {
			t.Fatal("task detail drift", response.Body.String(), err)
		}
	}
}

type rejectedCreator struct{ calls int }

func (s *rejectedCreator) CreateTask(context.Context, string, string, intake.CreateTask) (intake.CreateTaskResult, int, error) {
	s.calls++
	return intake.CreateTaskResult{}, 0, failure.New("internal_error")
}

func TestCreateHTTPMalformedResolutionPathsDoNotInvokeWorkflow(t *testing.T) {
	h, _, _ := receiveHandler(t)
	spy := &rejectedCreator{}
	h.creator = spy
	for _, path := range malformedResolutionPaths {
		w := command(h, "coordinator", "key", createBody, func(r *http.Request) { r.URL.Path = path })
		if w.Code != 404 || !strings.Contains(w.Body.String(), `"code":"not_found"`) {
			t.Fatal("malformed route accepted", path, w.Code, w.Body.String())
		}
	}
	if spy.calls != 0 {
		t.Fatal("malformed route invoked workflow", spy.calls)
	}
}

func TestCreateHTTPRejectsBeforeWorkflowAndReplay(t *testing.T) {
	h, store, diag := receiveHandler(t)
	spy := &rejectedCreator{}
	h.creator = spy
	for _, test := range []struct {
		name, role, body, code string
		status                 int
		change                 func(*http.Request)
	}{
		{name: "reader", role: "reader", status: 403, code: "forbidden"},
		{name: "worker", role: "worker", status: 403, code: "forbidden"},
		{name: "auth", status: 401, code: "unauthorized", change: func(r *http.Request) { r.Header.Set("Authorization", "Bearer PRIVATE_TOKEN") }},
		{name: "duplicate-auth", status: 401, code: "unauthorized", change: func(r *http.Request) { r.Header.Add("Authorization", r.Header.Get("Authorization")) }},
		{name: "host", status: 403, code: "forbidden", change: func(r *http.Request) { r.Host = "localhost:8765" }},
		{name: "origin", status: 403, code: "forbidden", change: func(r *http.Request) { r.Header.Set("Origin", "") }},
		{name: "version", status: 400, code: "unsupported_api_version", change: func(r *http.Request) { r.Header.Set("X-API-Version", "v2") }},
		{name: "duplicate-version", status: 400, code: "unsupported_api_version", change: func(r *http.Request) { r.Header.Add("X-API-Version", "v1"); r.Header.Add("X-API-Version", "v1") }},
		{name: "key", status: 400, code: "invalid_request", change: func(r *http.Request) { r.Header.Set("Idempotency-Key", "bad key") }},
		{name: "duplicate-key", status: 400, code: "invalid_request", change: func(r *http.Request) { r.Header.Add("Idempotency-Key", "second") }},
		{name: "missing-key", status: 400, code: "invalid_request", change: func(r *http.Request) { r.Header.Del("Idempotency-Key") }},
		{name: "media", status: 415, code: "unsupported_media_type", change: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{name: "duplicate-media", status: 415, code: "unsupported_media_type", change: func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }},
		{name: "query", status: 400, code: "invalid_request", change: func(r *http.Request) { r.URL.RawQuery = "token=PRIVATE_TOKEN" }},
		{name: "unknown", body: strings.TrimSuffix(createBody, "}") + `,"appliedBy":"PRIVATE_SPOOF"}`, status: 400, code: "invalid_request"},
		{name: "duplicate-json", body: strings.Replace(createBody, `"kind":"request"`, `"kind":"request","kind":"request"`, 1), status: 400, code: "invalid_request"},
		{name: "revision", body: strings.Replace(createBody, `"expectedMessageRevision":1`, `"expectedMessageRevision":9007199254740992`, 1), status: 400, code: "invalid_request"},
		{name: "kind", body: strings.Replace(createBody, `"kind":"request"`, `"kind":"code_review"`, 1), status: 400, code: "invalid_request"},
		{name: "action", body: strings.Replace(createBody, `"action":"createTask"`, `"action":"updateTask"`, 1), status: 400, code: "invalid_request"},
		{name: "null-task", body: `{"expectedMessageRevision":1,"action":"createTask","reason":"PRIVATE","task":null}`, status: 400, code: "invalid_request"},
		{name: "too-large", body: strings.Repeat(" ", v1.MaxBodyBytes+1), status: 413, code: "payload_too_large"},
		{name: "utf8", body: createBody + string([]byte{255}), status: 400, code: "invalid_request"},
		{name: "trailing", body: createBody + ` {}`, status: 400, code: "invalid_request"},
	} {
		t.Run(test.name, func(t *testing.T) {
			role := test.role
			if role == "" {
				role = "coordinator"
			}
			body := test.body
			if body == "" {
				body = createBody
			}
			w := command(h, role, "key", body, func(r *http.Request) {
				r.URL.Path = "/api/v1/inbox/messages/I-one/resolutions"
				if test.change != nil {
					test.change(r)
				}
			})
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal(w.Code, w.Body.String())
			}
			if _, err := v1.Validate("error", bytes.NewReader(w.Body.Bytes()), v1.MaxResponseBytes); err != nil {
				t.Fatal(err)
			}
			if len(w.Header().Get("X-Request-ID")) != 32 {
				t.Fatal("missing attempt ID")
			}
		})
	}
	if spy.calls != 0 {
		t.Fatal("untrusted request reached workflow/replay", spy.calls)
	}
	snapshot, err := store.LoadWorkspace(context.Background())
	if err != nil || snapshot.WorkspaceRevision != 1 || snapshot.Counts.Tasks != 0 || snapshot.Counts.Inbox != 0 {
		t.Fatal("rejected create changed state", err)
	}
	encoded, _ := json.Marshal(diag.Snapshot())
	if strings.Contains(string(encoded), "PRIVATE") {
		t.Fatal("private input entered diagnostics")
	}
}
