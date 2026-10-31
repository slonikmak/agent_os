// Package wailsread exposes saved-state queries with facade-owned panic recovery.
package wailsread

import (
	"context"
	"errors"
	"personaldesk/internal/application/failure"
	"time"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/queries"
)

// These are native view DTOs, independent of the versioned HTTP adapter.
type TaskSummary struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Title      string  `json:"title"`
	Summary    *string `json:"summary"`
	NextAction *string `json:"nextAction"`
	NextActor  *string `json:"nextActor"`
	Revision   int64   `json:"revision"`
	UpdatedAt  string  `json:"updatedAt"`
}
type InboxMetadata struct {
	ID         string  `json:"id"`
	Source     string  `json:"source"`
	Scope      string  `json:"scope"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Sender     *string `json:"sender"`
	OccurredAt *string `json:"occurredAt"`
	ReceivedAt string  `json:"receivedAt"`
	Revision   int64   `json:"revision"`
}
type SourceHealth struct {
	Source        string  `json:"source"`
	Scope         string  `json:"scope"`
	Status        string  `json:"status"`
	LastAttemptAt *string `json:"lastAttemptAt"`
	LastSuccessAt *string `json:"lastSuccessAt"`
}
type Counts struct {
	Tasks            int64 `json:"tasks"`
	Inbox            int64 `json:"inbox"`
	UnprocessedInbox int64 `json:"unprocessedInbox"`
	NeedsAttention   int64 `json:"needsAttention"`
}
type WorkspaceSnapshot struct {
	WorkspaceID       string          `json:"workspaceId"`
	WorkspaceRevision int64           `json:"workspaceRevision"`
	CreatedAt         string          `json:"createdAt"`
	Tasks             []TaskSummary   `json:"tasks"`
	Inbox             []InboxMetadata `json:"inbox"`
	Sources           []SourceHealth  `json:"sources"`
	Counts            Counts          `json:"counts"`
}
type Failure struct {
	Code            string  `json:"code"`
	Message         string  `json:"message"`
	Retryable       bool    `json:"retryable"`
	RequestID       string  `json:"requestId"`
	ErrorID         *string `json:"errorId"`
	CurrentRevision *int64  `json:"currentRevision"`
}
type Result struct {
	Data  *WorkspaceSnapshot `json:"data"`
	Error *Failure           `json:"error"`
}
type Facade struct {
	reader         queries.Reader
	diag           *diagnostics.Buffer
	startupCode    string
	startupErrorID string
}

func New(reader queries.Reader, diag *diagnostics.Buffer, startupCode string) *Facade {
	return &Facade{reader: reader, diag: diag, startupCode: startupCode}
}

func NewStartup(reader queries.Reader, diag *diagnostics.Buffer, code, errorID string) *Facade {
	f := New(reader, diag, code)
	f.startupErrorID = errorID
	return f
}

// Results carry typed failures instead of returning an error to Wails: Wails'
// outer error/panic serialization can otherwise expose raw exception strings.
// No lifecycle/resource owner is embedded or exported from the bound facade.
func (f *Facade) GetWorkspaceSnapshot(ctx context.Context) (result Result) {
	requestID := diagnostics.ID()
	defer f.diag.Operation("wailsread", "workspace.read", requestID, time.Now())
	defer func() {
		if recover() != nil {
			result = f.faultOperation(requestID, "workspace.read", "panic")
		}
	}()
	if f.reader == nil || f.diag.Status().RestartRequired {
		code := f.startupCode
		if code == "" {
			code = "service_unavailable"
		}
		var errorID *string
		if f.startupErrorID != "" {
			id := f.startupErrorID
			errorID = &id
		}
		return Result{Error: &Failure{Code: code, Message: "The workspace is unavailable", RequestID: requestID, ErrorID: errorID}}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	snapshot, err := f.reader.GetWorkspaceSnapshot(ctx)
	if err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return Result{Error: safe}
		}
		return f.fault(requestID)
	}
	view := WorkspaceSnapshot{WorkspaceID: snapshot.WorkspaceID, WorkspaceRevision: snapshot.WorkspaceRevision, CreatedAt: snapshot.CreatedAt, Tasks: []TaskSummary{}, Inbox: []InboxMetadata{}, Sources: []SourceHealth{}, Counts: Counts{snapshot.Counts.Tasks, snapshot.Counts.Inbox, snapshot.Counts.UnprocessedInbox, snapshot.Counts.NeedsAttention}}
	for _, t := range snapshot.Tasks {
		view.Tasks = append(view.Tasks, TaskSummary{t.ID, t.Kind, t.Status, t.Title, t.Summary, t.NextAction, t.NextActor, t.Revision, t.UpdatedAt})
	}
	for _, m := range snapshot.Inbox {
		view.Inbox = append(view.Inbox, InboxMetadata{m.ID, m.Source, m.Scope, m.Kind, m.Status, m.Sender, m.OccurredAt, m.ReceivedAt, m.Revision})
	}
	for _, s := range snapshot.Sources {
		view.Sources = append(view.Sources, SourceHealth{s.Source, s.Scope, s.Status, s.LastAttemptAt, s.LastSuccessAt})
	}
	if err := validateResponse("workspace", view); err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return Result{Error: safe}
		}
		return f.fault(requestID)
	}
	return Result{Data: &view}
}
func (f *Facade) fault(requestID string) Result {
	return f.faultOperation(requestID, "workspace.read", "internal")
}
func (f *Facade) faultOperation(requestID, operation, class string) Result {
	errorID := f.diag.Fault("wailsread", operation, requestID, class)
	return Result{Error: publicFailure(failure.New("internal_error"), requestID, &errorID)}
}

func expectedFailure(err error, requestID string) (*Failure, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return publicFailure(failure.New("service_unavailable"), requestID, nil), true
	}
	var expected *failure.Failure
	if errors.As(err, &expected) {
		safe := publicFailure(expected, requestID, nil)
		if safe.Code != "internal_error" {
			return safe, true
		}
	}
	return nil, false
}

func publicFailure(f *failure.Failure, requestID string, errorID *string) *Failure {
	safe := failure.Safe(f)
	return &Failure{Code: safe.Code, Message: safe.Message, Retryable: safe.Retryable, RequestID: requestID, ErrorID: errorID, CurrentRevision: safe.CurrentRevision}
}
