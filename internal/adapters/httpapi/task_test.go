package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
	"personaldesk/internal/testsupport/contractfixture"
	"strings"
	"testing"
)

type taskReaderStub struct {
	read func(context.Context, string) (queries.TaskDetails, error)
}

func (r taskReaderStub) GetWorkspaceSnapshot(context.Context) (queries.WorkspaceSnapshot, error) {
	return contractfixture.Snapshot(), nil
}
func (r taskReaderStub) GetTaskDetails(ctx context.Context, id string) (queries.TaskDetails, error) {
	return r.read(ctx, id)
}
func TestTaskHTTPStrictReadConfinementAndRoutes(t *testing.T) {
	h, _, diag := receiveHandler(t)
	for _, test := range []struct {
		name, path, body, code string
		status                 int
		change                 func(*http.Request)
	}{
		{name: "invalid-id", path: "/api/v1/tasks/%20", status: 400, code: "invalid_request"},
		{name: "missing", status: 404, code: "not_found"},
		{name: "extra-path", path: "/api/v1/tasks/T-one/extra", status: 404, code: "not_found"},
		{name: "trailing-slash", path: "/api/v1/tasks/T-one/", status: 404, code: "not_found"},
		{name: "host", status: 403, code: "forbidden", change: func(r *http.Request) { r.Host = "localhost:8765" }},
		{name: "origin", status: 403, code: "forbidden", change: func(r *http.Request) { r.Header.Set("Origin", "") }},
		{name: "auth", status: 401, code: "unauthorized", change: func(r *http.Request) { r.Header.Set("Authorization", "Bearer PRIVATE") }},
		{name: "duplicate-auth", status: 401, code: "unauthorized", change: func(r *http.Request) { r.Header.Add("Authorization", r.Header.Get("Authorization")) }},
		{name: "version", status: 400, code: "unsupported_api_version", change: func(r *http.Request) { r.Header.Set("X-API-Version", "v2") }},
		{name: "duplicate-version", status: 400, code: "unsupported_api_version", change: func(r *http.Request) { r.Header.Add("X-API-Version", "v1"); r.Header.Add("X-API-Version", "v1") }},
		{name: "query", status: 400, code: "invalid_request", change: func(r *http.Request) { r.URL.RawQuery = "token=PRIVATE" }},
		{name: "key", status: 400, code: "invalid_request", change: func(r *http.Request) { r.Header.Set("Idempotency-Key", "PRIVATE") }},
		{name: "media", status: 415, code: "unsupported_media_type", change: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{name: "duplicate-media", status: 415, code: "unsupported_media_type", change: func(r *http.Request) {
			r.Header.Add("Content-Type", "application/json")
			r.Header.Add("Content-Type", "application/json")
		}},
		{name: "body", body: "PRIVATE", status: 400, code: "invalid_request"},
		{name: "body-bound", body: strings.Repeat("x", v1.MaxBodyBytes+1), status: 413, code: "payload_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := test.path
			if path == "" {
				path = "/api/v1/tasks/T-missing"
			}
			r := httptest.NewRequest("GET", path, strings.NewReader(test.body))
			r.Host = "127.0.0.1:8765"
			r.Header.Set("Authorization", "Bearer "+testToken("reader"))
			r.Header.Set("X-Request-ID", "PRIVATE")
			if test.change != nil {
				test.change(r)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) || strings.Contains(w.Body.String(), "PRIVATE") || len(w.Header().Get("X-Request-ID")) != 32 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	data, _ := json.Marshal(diag.Snapshot())
	if strings.Contains(string(data), "PRIVATE") {
		t.Fatal("task read input logged")
	}
}
func TestTaskHTTPFaultBudgetAndCorruptSavedResults(t *testing.T) {
	for _, mode := range []string{"fault", "panic", "invalid", "budget", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			diag := diagnostics.New()
			reader := taskReaderStub{read: func(ctx context.Context, id string) (queries.TaskDetails, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				task := contractfixture.Task()
				switch mode {
				case "fault":
					return queries.TaskDetails{}, errors.New("PRIVATE SQL PATH")
				case "panic":
					panic("PRIVATE SQL PATH")
				case "invalid":
					task.CreatedAt = "PRIVATE PATH"
				case "budget":
					task.Timeline[0].Reason = strings.Repeat("x", v1.MaxResponseBytes)
				case "unavailable":
					return queries.TaskDetails{}, queries.Unavailable()
				}
				return queries.TaskDetails{WorkspaceRevision: 3, Task: task}, nil
			}}
			w := request(New(reader, testAuthenticator(t), "127.0.0.1:8765", diag), "/api/v1/tasks/T-0001", "127.0.0.1:8765", "reader", "", "")
			code := "internal_error"
			status := 500
			if mode == "budget" {
				code = "response_too_large"
				status = 503
			}
			if mode == "unavailable" {
				code = "service_unavailable"
				status = 503
			}
			if w.Code != status || !strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal(w.Code, w.Body.String())
			}
			var failure v1.ErrorEnvelope
			_ = json.Unmarshal(w.Body.Bytes(), &failure)
			if status == 500 && failure.Error.ErrorID == "" {
				t.Fatal("missing task fault ID")
			}
			data, _ := json.Marshal(diag.Snapshot())
			if strings.Contains(string(data), "PRIVATE") {
				t.Fatal("raw task fault logged")
			}
		})
	}
}
