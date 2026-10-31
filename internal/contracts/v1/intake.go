package v1

import (
	"encoding/json"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
)

// Presence preserves omission, null and value. Creation normalization occurs only
// in Input; future update commands must choose their documented preserve/clear rule.
type Presence[T any] struct {
	Set   bool
	Value *T
}

func (p *Presence[T]) UnmarshalJSON(data []byte) error {
	p.Set = true
	return json.Unmarshal(data, &p.Value)
}
func (p Presence[T]) MarshalJSON() ([]byte, error) { return json.Marshal(p.Value) }
func (p Presence[T]) IsZero() bool                 { return !p.Set }

type Receive struct {
	Source     string               `json:"source"`
	Scope      string               `json:"scope"`
	MessageID  string               `json:"messageId"`
	Kind       string               `json:"kind"`
	Sender     Presence[string]     `json:"sender,omitzero"`
	OccurredAt Presence[string]     `json:"occurredAt,omitzero"`
	Text       string               `json:"text"`
	SourceURL  Presence[string]     `json:"sourceUrl,omitzero"`
	TaskRef    Presence[TaskRef]    `json:"taskRef,omitzero"`
	Artifacts  []ArtifactCoordinate `json:"artifacts,omitempty"`
}
type NewTask struct {
	Kind       string           `json:"kind"`
	Title      string           `json:"title"`
	Summary    Presence[string] `json:"summary,omitzero"`
	NextAction Presence[string] `json:"nextAction,omitzero"`
	NextActor  Presence[string] `json:"nextActor,omitzero"`
}
type CreateTask struct {
	ExpectedMessageRevision int64   `json:"expectedMessageRevision"`
	Action                  string  `json:"action"`
	Reason                  string  `json:"reason"`
	Task                    NewTask `json:"task"`
}

func (r Receive) Input(p access.Principal) intake.Receive {
	result := intake.Receive{Source: r.Source, Scope: r.Scope, MessageID: r.MessageID, Kind: r.Kind, Text: r.Text, Sender: r.Sender.Value, OccurredAt: r.OccurredAt.Value, SourceURL: r.SourceURL.Value, Artifacts: []queries.ArtifactCoordinate{}, SubmittedBy: p}
	if r.TaskRef.Value != nil {
		result.TaskRef = &queries.TaskRef{TaskID: r.TaskRef.Value.TaskID, Revision: r.TaskRef.Value.Revision}
	}
	for _, a := range r.Artifacts {
		result.Artifacts = append(result.Artifacts, queries.ArtifactCoordinate{Kind: a.Kind, Coordinate: a.Coordinate, Label: a.Label})
	}
	return result
}
func (r CreateTask) Input(p access.Principal) intake.CreateTask {
	return intake.CreateTask{ExpectedMessageRevision: r.ExpectedMessageRevision, Action: r.Action, Reason: r.Reason, Task: intake.NewTask{Kind: r.Task.Kind, Title: r.Task.Title, Summary: r.Task.Summary.Value, NextAction: r.Task.NextAction.Value, NextActor: r.Task.NextActor.Value}, AppliedBy: p}
}

type ReceiveResult struct {
	WorkspaceRevision int64        `json:"workspaceRevision"`
	Message           InboxMessage `json:"message"`
}
type CreateTaskResult struct {
	WorkspaceRevision int64        `json:"workspaceRevision"`
	Message           InboxMessage `json:"message"`
	Task              Task         `json:"task"`
}

func FromReceiveResult(r intake.ReceiveResult) ReceiveResult {
	return ReceiveResult{r.WorkspaceRevision, FromInboxMessage(r.Message)}
}
func FromCreateTaskResult(r intake.CreateTaskResult) CreateTaskResult {
	return CreateTaskResult{r.WorkspaceRevision, FromInboxMessage(r.Message), FromTask(r.Task)}
}
