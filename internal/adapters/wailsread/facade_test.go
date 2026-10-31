package wailsread

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"personaldesk/internal/adapters/httpapi"
	"personaldesk/internal/application/access"
	"personaldesk/internal/testsupport/contractfixture"
	"reflect"
	"strings"
	"testing"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
)

type readerFunc func(context.Context) (queries.WorkspaceSnapshot, error)

func (f readerFunc) GetWorkspaceSnapshot(ctx context.Context) (queries.WorkspaceSnapshot, error) {
	return f(ctx)
}
func TestFacadeOwnsFaultsAndExposesOnlyQuery(t *testing.T) {
	for _, reader := range []readerFunc{
		func(context.Context) (queries.WorkspaceSnapshot, error) { panic("SECRET SQL /private/home") },
		func(context.Context) (queries.WorkspaceSnapshot, error) {
			return queries.WorkspaceSnapshot{}, errors.New("SECRET SQL /private/home")
		},
	} {
		diag := diagnostics.New()
		result := New(reader, diag, "").GetWorkspaceSnapshot(context.Background())
		if result.Error == nil || result.Error.Code != "internal_error" || result.Error.ErrorID == nil || result.Data != nil {
			t.Fatalf("result=%+v", result)
		}
		encoded, _ := json.Marshal(result)
		records, _ := json.Marshal(diag.Snapshot())
		if strings.Contains(string(encoded)+string(records), "SECRET") || strings.Contains(string(encoded)+string(records), "/private") {
			t.Fatal("raw error leaked")
		}
	}
	methods := reflect.TypeOf(&Facade{})
	if methods.NumMethod() != 4 || methods.Method(0).Name != "GetInboxMessage" || methods.Method(1).Name != "GetTaskDetails" || methods.Method(2).Name != "GetWorkspaceSnapshot" || methods.Method(3).Name != "ReadMaterial" {
		t.Fatal("broad facade bound")
	}
}
func TestTransportMappingsAgreeAndNullsAreExplicit(t *testing.T) {
	s := queries.WorkspaceSnapshot{WorkspaceID: "identity", WorkspaceRevision: 1, CreatedAt: "2026-10-29T00:00:00Z", Tasks: []queries.TaskSummary{{ID: "T-1", Kind: "request", Status: "new", Title: "test", Revision: 1, UpdatedAt: "2026-10-29T00:00:00Z"}}, Inbox: []queries.InboxMetadata{}, Sources: []queries.SourceHealth{}}
	result := New(readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) { return s, nil }), diagnostics.New(), "").GetWorkspaceSnapshot(context.Background())
	native, _ := json.Marshal(result.Data)
	http, _ := json.Marshal(v1.FromSnapshot(s))
	if string(native) != string(http) {
		t.Fatalf("transport mappings differ: %s; %s", native, http)
	}
	if !strings.Contains(string(native), `"summary":null`) || !strings.Contains(string(native), `"inbox":[]`) {
		t.Fatal("null/empty representation drift")
	}
}

func TestPopulatedAndDetailMappingsShareSchemaFactsWithoutBindingNewMethods(t *testing.T) {
	snapshot := contractfixture.Snapshot()
	result := New(readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) { return snapshot, nil }), diagnostics.New(), "").GetWorkspaceSnapshot(context.Background())
	cases := []struct {
		schema       string
		native, http any
	}{
		{"workspace", result.Data, v1.FromSnapshot(snapshot)},
		{"task-details", FromTaskDetails(queries.TaskDetails{WorkspaceRevision: 3, Task: contractfixture.Task()}), v1.FromTaskDetails(queries.TaskDetails{WorkspaceRevision: 3, Task: contractfixture.Task()})},
		{"inbox-details", FromInboxDetails(queries.InboxDetails{WorkspaceRevision: 2, Message: contractfixture.Message()}), v1.FromInboxDetails(queries.InboxDetails{WorkspaceRevision: 2, Message: contractfixture.Message()})},
		{"material", FromMaterialResult(queries.MaterialResult{WorkspaceRevision: 3, Material: queries.Material{ID: "M-0001", Availability: "unavailable", UnavailableReason: ptr("missing")}}), v1.FromMaterialResult(queries.MaterialResult{WorkspaceRevision: 3, Material: queries.Material{ID: "M-0001", Availability: "unavailable", UnavailableReason: ptr("missing")}})},
	}
	for _, test := range cases {
		native, _ := json.Marshal(test.native)
		http, _ := json.Marshal(test.http)
		if string(native) != string(http) {
			t.Fatalf("%s mapping drift", test.schema)
		}
		if _, err := v1.Validate(test.schema, bytes.NewReader(native), v1.MaxResponseBytes); err != nil {
			t.Fatalf("%s mapping fails schema: %s", test.schema, native)
		}
	}
	applied := FromInboxMessage(contractfixture.AppliedMessage())
	if applied.SubmittedBy.ClientID == applied.Resolution.AppliedBy.ClientID || applied.ProposedTaskRef != nil || applied.Resolution.TaskID != "T-0001" {
		t.Fatal("authenticated/claimed/proposed/confirmed identities collapsed")
	}
	proposed := contractfixture.Message()
	proposed.ProposedTaskRef = &queries.TaskRef{TaskID: "T-unconfirmed", Revision: 2}
	mapped := FromInboxMessage(proposed)
	if mapped.Resolution != nil || mapped.ProposedTaskRef.TaskID != "T-unconfirmed" {
		t.Fatal("proposed association rendered as applied")
	}
}
func ptr(value string) *string { return &value }

func TestHTTPAndWailsErrorsHaveEquivalentSafeApplicationFacts(t *testing.T) {
	revision := int64(2)
	for _, expected := range []*queries.Failure{{Code: "not_found", Message: "PRIVATE"}, {Code: "service_unavailable", Message: "PRIVATE"}, {Code: "revision_conflict", Message: "PRIVATE", CurrentRevision: &revision}, {Code: "unknown PRIVATE", Message: "PRIVATE"}, {Code: "internal_error", Message: "PRIVATE"}} {
		reader := readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) { return queries.WorkspaceSnapshot{}, expected })
		auth, err := access.New([]access.Credential{{Role: access.Reader, Token: strings.Repeat("1", 64)}, {Role: access.Worker, Token: strings.Repeat("2", 64)}, {Role: access.Coordinator, Token: strings.Repeat("3", 64)}})
		if err != nil {
			t.Fatal(err)
		}
		native := New(reader, diagnostics.New(), "").GetWorkspaceSnapshot(context.Background())
		r := httptest.NewRequest("GET", "/api/v1/workspace", nil)
		r.Host = "127.0.0.1:8765"
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("1", 64))
		r.Header.Set("X-Request-ID", "PRIVATE")
		w := httptest.NewRecorder()
		httpapi.New(reader, auth, r.Host, diagnostics.New()).ServeHTTP(w, r)
		var wire v1.ErrorEnvelope
		if err = json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		if native.Error == nil || native.Data != nil || native.Error.Code != wire.Error.Code || native.Error.Message != wire.Error.Message || native.Error.Retryable != wire.Error.Retryable || !reflect.DeepEqual(native.Error.CurrentRevision, wire.Error.CurrentRevision) {
			t.Fatalf("error drift native=%+v http=%+v", native.Error, wire.Error)
		}
		if len(native.Error.RequestID) != 32 || wire.Error.RequestID != w.Header().Get("X-Request-ID") || wire.Error.RequestID == native.Error.RequestID {
			t.Fatal("attempt IDs are not server-derived")
		}
		if (native.Error.ErrorID != nil) != (wire.Error.ErrorID != "") {
			t.Fatal("unexpected/expected fault identity drift")
		}
		if _, err = v1.Validate("error", bytes.NewReader(w.Body.Bytes()), v1.MaxResponseBytes); err != nil {
			t.Fatalf("error schema mismatch: %s", w.Body.String())
		}
		encoded, _ := json.Marshal(native)
		if strings.Contains(string(encoded)+w.Body.String(), "PRIVATE") {
			t.Fatal("private error leaked")
		}
	}
	huge := contractfixture.Snapshot()
	huge.Tasks[0].Title = strings.Repeat("x", v1.MaxResponseBytes)
	diag := diagnostics.New()
	result := New(readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) { return huge, nil }), diag, "").GetWorkspaceSnapshot(context.Background())
	if result.Data != nil || result.Error == nil || result.Error.Code != "response_too_large" || !result.Error.Retryable {
		t.Fatal("native response budget disagrees with HTTP")
	}
	assertNativeFailure(t, diag, result.Error, "workspace.read", "response_too_large", "")
}

func TestInvalidSavedSnapshotCannotEscapeEitherBoundary(t *testing.T) {
	snapshot := contractfixture.Snapshot()
	snapshot.Tasks[0].UpdatedAt = "PRIVATE /home"
	reader := readerFunc(func(context.Context) (queries.WorkspaceSnapshot, error) { return snapshot, nil })
	diag := diagnostics.New()
	native := New(reader, diag, "").GetWorkspaceSnapshot(context.Background())
	if native.Error == nil || native.Error.Code != "internal_error" || native.Data != nil {
		t.Fatal("native returned malformed saved facts")
	}
	assertNativeFailure(t, diag, native.Error, "workspace.read", "internal_error", "internal")
	auth, _ := access.New([]access.Credential{{Role: access.Reader, Token: strings.Repeat("1", 64)}, {Role: access.Worker, Token: strings.Repeat("2", 64)}, {Role: access.Coordinator, Token: strings.Repeat("3", 64)}})
	r := httptest.NewRequest("GET", "/api/v1/workspace", nil)
	r.Host = "127.0.0.1:8765"
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("1", 64))
	w := httptest.NewRecorder()
	httpapi.New(reader, auth, r.Host, diagnostics.New()).ServeHTTP(w, r)
	if w.Code != 500 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "/home") {
		t.Fatal("HTTP returned malformed/private saved facts")
	}
}

func TestFaultsUseTheirActualBoundaryAndExpectedCancellationHasNoFault(t *testing.T) {
	for _, operation := range []string{"workspace.read", "task.read", "inbox.read", "material.read"} {
		for _, mode := range []string{"panic", "canceled", "deadline"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				diag := diagnostics.New()
				outcome := func(ctx context.Context) error {
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("missing bounded query context")
					}
					switch mode {
					case "panic":
						panic("SYNTHETIC_SECRET /private")
					case "canceled":
						return context.Canceled
					default:
						return context.DeadlineExceeded
					}
				}
				var result *Failure
				switch operation {
				case "workspace.read":
					result = New(readerFunc(func(ctx context.Context) (queries.WorkspaceSnapshot, error) {
						return queries.WorkspaceSnapshot{}, outcome(ctx)
					}), diag, "").GetWorkspaceSnapshot(context.Background()).Error
				case "task.read":
					result = New(taskReaderStub{read: func(ctx context.Context, _ string) (queries.TaskDetails, error) {
						return queries.TaskDetails{}, outcome(ctx)
					}}, diag, "").GetTaskDetails(context.Background(), "T-one").Error
				case "inbox.read":
					result = New(inboxReaderStub{read: func(ctx context.Context, _ string) (queries.InboxDetails, error) {
						return queries.InboxDetails{}, outcome(ctx)
					}}, diag, "").GetInboxMessage(context.Background(), "I-one").Error
				case "material.read":
					result = New(materialReaderStub{read: func(ctx context.Context, _ string) (queries.MaterialResult, error) {
						return queries.MaterialResult{}, outcome(ctx)
					}}, diag, "").ReadMaterial(context.Background(), "M-one").Error
				}
				code, class := "service_unavailable", ""
				if mode == "panic" {
					code, class = "internal_error", "panic"
				}
				assertNativeFailure(t, diag, result, operation, code, class)
			})
		}
	}
	methods := reflect.TypeOf(&DiagnosticFacade{})
	if methods.NumMethod() != 3 || methods.Method(0).Name != "ExportDiagnostics" || methods.Method(1).Name != "GetDiagnosticStatus" || methods.Method(2).Name != "ReportFrontendFault" {
		t.Fatal("broad operational facade")
	}
}

func assertNativeFailure(t *testing.T, diag *diagnostics.Buffer, result *Failure, operation, code, class string) {
	t.Helper()
	if result == nil || result.Code != code || len(result.RequestID) != 32 || (result.ErrorID != nil) != (class != "") {
		t.Fatalf("unexpected native failure: %+v", result)
	}
	faults, operations := 0, 0
	for _, event := range diag.Snapshot() {
		if event.Code == "query.fault" {
			faults++
			if class == "" || event.Component != "wailsread" || event.Operation != operation || event.ErrorID != *result.ErrorID || event.RequestID != result.RequestID || event.ErrorClass != class {
				t.Fatal("wrong fault correlation", operation, event)
			}
		}
		if event.Code == "api.operation" {
			operations++
			if event.Component != "wailsread" || event.Operation != operation || event.RequestID != result.RequestID {
				t.Fatal("wrong attempt correlation", operation, event)
			}
		}
	}
	if operations != 1 || (class == "" && faults != 0) || (class != "" && faults != 1) {
		t.Fatalf("unexpected operation/fault counts: %d/%d", operations, faults)
	}
	raw, _ := json.Marshal(struct {
		Result *Failure
		Events []diagnostics.Event
	}{result, diag.Snapshot()})
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "/private") {
		t.Fatal("raw fault content leaked")
	}
}

func TestFailedStartupReadsKeepFreshAttemptCorrelation(t *testing.T) {
	diag := diagnostics.New()
	startupRequestID := diagnostics.ID()
	startupErrorID := diag.Fault("bootstrap", "startup", startupRequestID, "database")
	facade := NewStartup(nil, diag, "database_unsupported", startupErrorID)
	responses := []Result{facade.GetWorkspaceSnapshot(context.Background()), facade.GetWorkspaceSnapshot(context.Background())}
	for _, response := range responses {
		if response.Data != nil || response.Error == nil || response.Error.Code != "database_unsupported" || response.Error.ErrorID == nil || *response.Error.ErrorID != startupErrorID {
			t.Fatal("startup error identity/category changed")
		}
		if len(response.Error.RequestID) != 32 || response.Error.RequestID == startupRequestID {
			t.Fatal("read reused original startup request ID")
		}
	}
	if responses[0].Error.RequestID == responses[1].Error.RequestID {
		t.Fatal("failed-start reads reused an attempt request ID")
	}
	operations := []diagnostics.Event{}
	for _, event := range diag.Snapshot() {
		data, _ := json.Marshal(event)
		if _, err := v1.Validate("diagnostic-event", bytes.NewReader(data), v1.MaxDiagnosticRecordBytes); err != nil {
			t.Fatal("invalid generated correlation", err)
		}
		if event.Code == "api.operation" && event.Component == "wailsread" && event.Operation == "workspace.read" {
			operations = append(operations, event)
		}
		if event.ErrorID == startupErrorID && event.RequestID != startupRequestID {
			t.Fatal("original startup fault correlation changed")
		}
	}
	if len(operations) != len(responses) {
		t.Fatal("missing read attempt milestones")
	}
	for i, event := range operations {
		if event.RequestID != responses[i].Error.RequestID {
			t.Fatal("response and operation attempt IDs differ")
		}
	}
}
