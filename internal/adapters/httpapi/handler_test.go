package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
)

type readerFunc func(context.Context) (queries.WorkspaceSnapshot, error)

func (f readerFunc) GetWorkspaceSnapshot(ctx context.Context) (queries.WorkspaceSnapshot, error) {
	return f(ctx)
}
func request(handler http.Handler, path, host, token, origin, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, strings.NewReader(body))
	r.Host = host
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+testToken(token))
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("X-Request-ID", "UNTRUSTED")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func TestAuthenticationConfinementAndDiscovery(t *testing.T) {
	calls := 0
	h := New(readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) {
		calls++
		return queries.WorkspaceSnapshot{WorkspaceID: "id", WorkspaceRevision: 1, CreatedAt: "2026-10-29T00:00:00Z"}, nil
	}), testAuthenticator(t), "127.0.0.1:8765", diagnostics.New())
	for _, test := range []struct {
		host, token, origin string
		status              int
	}{
		{"127.0.0.1:8765", "", "", 401}, {"127.0.0.1:8765", "wrong", "", 401}, {"localhost:8765", "reader", "", 403}, {"127.0.0.1:8765", "reader", "http://evil.example", 403},
	} {
		w := request(h, "/api/v1/workspace", test.host, test.token, test.origin, "")
		if w.Code != test.status {
			t.Fatalf("status=%d wanted=%d", w.Code, test.status)
		}
		if len(w.Header().Get("X-Request-ID")) != 32 || w.Header().Get("X-Request-ID") == "UNTRUSTED" {
			t.Fatal("untrusted request ID")
		}
	}
	if calls != 0 {
		t.Fatal("untrusted request reached query")
	}
	for _, token := range []string{"reader", "worker", "coordinator"} {
		w := request(h, "/api/v1/workspace", "127.0.0.1:8765", token, "", "")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	w := request(h, "/api/v1", "127.0.0.1:8765", "reader", "", "")
	var discovery v1.Discovery
	if err := json.Unmarshal(w.Body.Bytes(), &discovery); err != nil || len(discovery.Capabilities) != 1 || discovery.Capabilities[0] != "workspace.read" {
		t.Fatalf("discovery=%+v error=%v", discovery, err)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unsafe response headers")
	}
}
func TestFaultPrivacyBoundsAndUnknownRoutes(t *testing.T) {
	diag := diagnostics.New()
	for _, reader := range []readerFunc{
		func(context.Context) (queries.WorkspaceSnapshot, error) { panic("TOKEN BUSINESS SQL /private/home") },
		func(context.Context) (queries.WorkspaceSnapshot, error) {
			return queries.WorkspaceSnapshot{}, errors.New("TOKEN BUSINESS SQL /private/home")
		},
	} {
		h := New(reader, testAuthenticator(t), "127.0.0.1:8765", diag)
		w := request(h, "/api/v1/workspace", "127.0.0.1:8765", "reader", "", "")
		var envelope v1.ErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || w.Code != 500 || envelope.Error.ErrorID == "" || envelope.Error.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatalf("fault=%s", w.Body.String())
		}
		records, _ := json.Marshal(diag.Snapshot())
		for _, marker := range []string{"TOKEN", "BUSINESS", "SQL", "/private"} {
			if strings.Contains(w.Body.String()+string(records), marker) {
				t.Fatalf("leaked %s", marker)
			}
		}
	}
	h := New(readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) {
		return queries.WorkspaceSnapshot{WorkspaceID: "id", WorkspaceRevision: 1, Tasks: []queries.TaskSummary{{Title: strings.Repeat("x", v1.MaxResponseBytes)}}}, nil
	}), testAuthenticator(t), "127.0.0.1:8765", diag)
	for _, test := range []struct {
		path, body, code string
		status           int
	}{
		{"/api/v1/workspace", "", "response_too_large", 503},
		{"/api/v1/workspace", strings.Repeat("x", v1.MaxBodyBytes+1), "payload_too_large", 413},
		{"/api/v1/workspace", "{}", "invalid_request", 400},
		{"/api/v1/workspace?token=SECRET", "", "invalid_request", 400},
		{"/api/v1/execute", "", "not_found", 404},
		{"/api/version", "", "not_found", 404},
		{"/api/v1x", "", "not_found", 404},
		{"/api/v2/workspace", "", "unsupported_api_version", 400},
	} {
		w := request(h, test.path, "127.0.0.1:8765", "reader", "", test.body)
		if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
func TestCancellationReachesQuery(t *testing.T) {
	h := New(readerFunc(func(ctx context.Context) (queries.WorkspaceSnapshot, error) {
		<-ctx.Done()
		return queries.WorkspaceSnapshot{}, queries.Unavailable()
	}), testAuthenticator(t), "127.0.0.1:8765", diagnostics.New())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/api/v1/workspace", nil).WithContext(ctx)
	r.Host = "127.0.0.1:8765"
	r.Header.Set("Authorization", "Bearer "+testToken("reader"))
	r.Body = io.NopCloser(strings.NewReader(""))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
}

func testToken(role string) string {
	switch role {
	case "reader":
		return strings.Repeat("1", 64)
	case "worker":
		return strings.Repeat("2", 64)
	case "coordinator":
		return strings.Repeat("3", 64)
	default:
		return role
	}
}
func testAuthenticator(t *testing.T) *access.Authenticator {
	t.Helper()
	auth, err := access.New([]access.Credential{{Role: access.Reader, Token: testToken("reader")}, {Role: access.Worker, Token: testToken("worker")}, {Role: access.Coordinator, Token: testToken("coordinator")}})
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func TestPrincipalSurvivesAuthenticationAndDisabledRoutesStayUnavailable(t *testing.T) {
	calls := 0
	h := New(readerFunc(func(ctx context.Context) (queries.WorkspaceSnapshot, error) {
		calls++
		p, ok := access.FromContext(ctx)
		if !ok || !access.Allowed(p, access.ReadWorkspace) {
			t.Fatal("principal lost before query")
		}
		return queries.WorkspaceSnapshot{WorkspaceID: "id", WorkspaceRevision: 1, CreatedAt: "2026-10-29T00:00:00Z"}, nil
	}), testAuthenticator(t), "127.0.0.1:8765", diagnostics.New())
	for _, role := range []string{"reader", "worker", "coordinator"} {
		if w := request(h, "/api/v1/workspace", "127.0.0.1:8765", role, "", ""); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		for _, path := range []string{"/api/v1/tasks/T-1", "/api/v1/inbox/messages/I-1", "/api/v1/materials/M-1", "/api/v1/inbox/messages", "/api/v1/inbox/messages/I-1/resolutions", "/api/v1/diagnostics/export", "/api/v1/provider/execute"} {
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"secret":"PRIVATE"}`))
			r.Host = "127.0.0.1:8765"
			r.Header.Set("Authorization", "Bearer "+testToken(role))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 404 || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatalf("disabled route %s role %s returned %d", path, role, w.Code)
			}
		}
	}
	if calls != 3 {
		t.Fatalf("disabled route reached query: %d", calls)
	}
}

func TestDuplicateAndBadHeadersFailBeforeQueries(t *testing.T) {
	calls := 0
	h := New(readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) {
		calls++
		return queries.WorkspaceSnapshot{}, nil
	}), testAuthenticator(t), "127.0.0.1:8765", diagnostics.New())
	for _, test := range []struct {
		name   string
		header http.Header
		status int
	}{
		{"duplicate-auth", http.Header{"Authorization": []string{"Bearer " + testToken("reader"), "Bearer " + testToken("worker")}}, 401},
		{"empty-origin", http.Header{"Origin": []string{""}}, 403},
		{"duplicate-host", http.Header{"Host": []string{"127.0.0.1:8765"}}, 403},
		{"duplicate-version", http.Header{"X-Api-Version": []string{"v1", "v1"}}, 400},
		{"wrong-version", http.Header{"X-Api-Version": []string{"v2 PRIVATE"}}, 400},
		{"idempotency-on-read", http.Header{"Idempotency-Key": []string{"PRIVATE"}}, 400},
		{"bad-media", http.Header{"Content-Type": []string{"text/plain PRIVATE"}}, 415},
		{"duplicate-media", http.Header{"Content-Type": []string{"application/json", "application/json"}}, 415},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/workspace", nil)
			r.Host = "127.0.0.1:8765"
			r.Header.Set("Authorization", "Bearer "+testToken("reader"))
			for key, value := range test.header {
				r.Header[key] = value
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.status || strings.Contains(w.Body.String(), "PRIVATE") || len(w.Header().Get("X-Request-ID")) != 32 {
				t.Fatalf("response %d %s", w.Code, w.Body.String())
			}
		})
	}
	if calls != 0 {
		t.Fatal("invalid headers reached query")
	}
}

func TestFutureMutationHeaderBoundary(t *testing.T) {
	valid := http.Header{"Authorization": []string{"Bearer " + testToken("worker")}, "Idempotency-Key": []string{"synthetic-key-1"}, "Content-Type": []string{"application/json"}}
	if f := ValidateMutationHeaders(valid); f != nil {
		t.Fatal(f)
	}
	for _, test := range []struct {
		key    string
		values []string
		code   string
	}{
		{"Authorization", nil, "unauthorized"}, {"Authorization", []string{"Bearer PRIVATE"}, "unauthorized"}, {"Authorization", []string{"Bearer " + testToken("worker"), "Bearer " + testToken("worker")}, "unauthorized"},
		{"Idempotency-Key", nil, "invalid_request"}, {"Idempotency-Key", []string{"PRIVATE KEY"}, "invalid_request"}, {"Idempotency-Key", []string{strings.Repeat("x", 129)}, "invalid_request"}, {"Idempotency-Key", []string{"a", "a"}, "invalid_request"},
		{"Content-Type", nil, "unsupported_media_type"}, {"Content-Type", []string{"text/plain"}, "unsupported_media_type"}, {"Content-Type", []string{"application/json", "application/json"}, "unsupported_media_type"},
		{"X-Api-Version", []string{"v2"}, "unsupported_api_version"}, {"X-Api-Version", []string{"v1", "v1"}, "unsupported_api_version"},
	} {
		header := valid.Clone()
		if test.values == nil {
			header.Del(test.key)
		} else {
			header[test.key] = test.values
		}
		f := ValidateMutationHeaders(header)
		if f == nil || f.Code != test.code || strings.Contains(f.Message, "PRIVATE") {
			t.Fatalf("header %s got %+v", test.key, f)
		}
	}
}
