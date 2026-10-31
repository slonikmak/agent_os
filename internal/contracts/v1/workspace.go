// Package v1 defines the frozen E1 wire inventory; discovery lists enabled handlers only.
package v1

import "personaldesk/internal/application/queries"

const MaxBodyBytes = 1 << 20
const MaxHeaderBytes = 16 << 10
const MaxCredentialBytes = 4096
const MaxResponseBytes = 4 << 20

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

func FromSnapshot(s queries.WorkspaceSnapshot) WorkspaceSnapshot {
	result := WorkspaceSnapshot{WorkspaceID: s.WorkspaceID, WorkspaceRevision: s.WorkspaceRevision, CreatedAt: s.CreatedAt, Tasks: []TaskSummary{}, Inbox: []InboxMetadata{}, Sources: []SourceHealth{}, Counts: Counts{s.Counts.Tasks, s.Counts.Inbox, s.Counts.UnprocessedInbox, s.Counts.NeedsAttention}}
	for _, t := range s.Tasks {
		result.Tasks = append(result.Tasks, TaskSummary{t.ID, t.Kind, t.Status, t.Title, t.Summary, t.NextAction, t.NextActor, t.Revision, t.UpdatedAt})
	}
	for _, m := range s.Inbox {
		result.Inbox = append(result.Inbox, InboxMetadata{m.ID, m.Source, m.Scope, m.Kind, m.Status, m.Sender, m.OccurredAt, m.ReceivedAt, m.Revision})
	}
	for _, h := range s.Sources {
		result.Sources = append(result.Sources, SourceHealth{h.Source, h.Scope, h.Status, h.LastAttemptAt, h.LastSuccessAt})
	}
	return result
}

type Error struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	Retryable       bool   `json:"retryable"`
	RequestID       string `json:"requestId"`
	ErrorID         string `json:"errorId,omitempty"`
	CurrentRevision *int64 `json:"currentRevision,omitempty"`
}
type ErrorEnvelope struct {
	Error Error `json:"error"`
}
type Limits struct {
	MaxRequestBodyBytes int `json:"maxRequestBodyBytes"`
	MaxResponseBytes    int `json:"maxResponseBytes"`
}
type Discovery struct {
	APIVersion   string   `json:"apiVersion"`
	APIRevision  int      `json:"apiRevision"`
	Capabilities []string `json:"capabilities"`
	Limits       Limits   `json:"limits"`
}

func Describe() Discovery {
	return Discovery{"v1", 1, []string{"workspace.read"}, Limits{MaxBodyBytes, MaxResponseBytes}}
}
