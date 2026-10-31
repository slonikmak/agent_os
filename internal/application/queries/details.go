package queries

import "personaldesk/internal/application/access"

// Shared saved-detail models. Coordinates carry no file authority.
type TaskRef struct {
	TaskID   string
	Revision int64
}
type ArtifactCoordinate struct {
	Kind, Coordinate string
	Label            *string
}
type MaterialAssociation struct {
	Coordinate        ArtifactCoordinate
	MaterialID        *string
	Availability      string
	UnavailableReason *string
}
type Provenance struct {
	MessageID   string
	SubmittedBy access.Principal
}
type Resolution struct {
	ID, Action, Reason, TaskID string
	AppliedBy                  access.Principal
	AppliedAt                  string
}
type InboxMessage struct {
	InboxMetadata
	MessageID, Text string
	SourceURL       *string
	Artifacts       []MaterialAssociation
	SubmittedBy     access.Principal
	ProposedTaskRef *TaskRef
	Resolution      *Resolution
}
type TaskEvent struct {
	ID, Kind, At, Reason string
	Provenance           Provenance
	AppliedBy            access.Principal
}
type Task struct {
	TaskSummary
	CreatedAt  string
	Provenance *Provenance
	Timeline   []TaskEvent
	Materials  []MaterialAssociation
}
type TaskDetails struct {
	WorkspaceRevision int64
	Task              Task
}
type InboxDetails struct {
	WorkspaceRevision int64
	Message           InboxMessage
}
type Material struct {
	ID, Availability                   string
	MediaType, Text, UnavailableReason *string
}
type MaterialResult struct {
	WorkspaceRevision int64
	Material          Material
}
