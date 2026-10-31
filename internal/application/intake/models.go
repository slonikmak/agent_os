// Package intake owns local creation inputs, results and receive transaction orchestration.
package intake

import (
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/queries"
	"personaldesk/internal/application/tasks"
)

// New records normalize omitted nullable values to nil and omitted artifacts to [].
// Claimed Source/Sender never replace the authenticated SubmittedBy principal.
type Receive struct {
	Source, Scope, MessageID, Kind, Text string
	Sender, OccurredAt, SourceURL        *string
	TaskRef                              *queries.TaskRef
	Artifacts                            []queries.ArtifactCoordinate
	SubmittedBy                          access.Principal
}
type NewTask = tasks.NewTask
type CreateTask struct {
	ExpectedMessageRevision int64
	Action, Reason          string
	Task                    NewTask
	AppliedBy               access.Principal
}
type ReceiveResult struct {
	WorkspaceRevision int64
	Message           queries.InboxMessage
}
type CreateTaskResult struct {
	WorkspaceRevision int64
	Message           queries.InboxMessage
	Task              queries.Task
}
