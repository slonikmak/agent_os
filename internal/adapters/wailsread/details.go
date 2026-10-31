package wailsread

import (
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/queries"
)

type Principal struct {
	ClientID string `json:"clientId"`
	Role     string `json:"role"`
}
type TaskRef struct {
	TaskID   string `json:"taskId"`
	Revision int64  `json:"revision"`
}
type ArtifactCoordinate struct {
	Kind       string  `json:"kind"`
	Coordinate string  `json:"coordinate"`
	Label      *string `json:"label"`
}
type MaterialAssociation struct {
	Coordinate        ArtifactCoordinate `json:"coordinate"`
	MaterialID        *string            `json:"materialId"`
	Availability      string             `json:"availability"`
	UnavailableReason *string            `json:"unavailableReason"`
}
type Provenance struct {
	MessageID   string    `json:"messageId"`
	SubmittedBy Principal `json:"submittedBy"`
}
type Resolution struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	TaskID    string    `json:"taskId"`
	AppliedBy Principal `json:"appliedBy"`
	AppliedAt string    `json:"appliedAt"`
}
type InboxMessage struct {
	InboxMetadata
	MessageID       string                `json:"messageId"`
	Text            string                `json:"text"`
	SourceURL       *string               `json:"sourceUrl"`
	Artifacts       []MaterialAssociation `json:"artifacts"`
	SubmittedBy     Principal             `json:"submittedBy"`
	ProposedTaskRef *TaskRef              `json:"proposedTaskRef"`
	Resolution      *Resolution           `json:"resolution"`
}
type TaskEvent struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	At         string     `json:"at"`
	Reason     string     `json:"reason"`
	Provenance Provenance `json:"provenance"`
	AppliedBy  Principal  `json:"appliedBy"`
}
type Task struct {
	TaskSummary
	CreatedAt  string                `json:"createdAt"`
	Provenance *Provenance           `json:"provenance"`
	Timeline   []TaskEvent           `json:"timeline"`
	Materials  []MaterialAssociation `json:"materials"`
}
type TaskDetails struct {
	WorkspaceRevision int64 `json:"workspaceRevision"`
	Task              Task  `json:"task"`
}
type InboxDetails struct {
	WorkspaceRevision int64        `json:"workspaceRevision"`
	Message           InboxMessage `json:"message"`
}
type Material struct {
	ID                string  `json:"id"`
	Availability      string  `json:"availability"`
	MediaType         *string `json:"mediaType"`
	Text              *string `json:"text"`
	UnavailableReason *string `json:"unavailableReason"`
}
type MaterialResult struct {
	WorkspaceRevision int64    `json:"workspaceRevision"`
	Material          Material `json:"material"`
}

func FromPrincipal(p access.Principal) Principal { return Principal{p.ClientID, string(p.Role)} }
func FromMaterialAssociation(a queries.MaterialAssociation) MaterialAssociation {
	return MaterialAssociation{ArtifactCoordinate{a.Coordinate.Kind, a.Coordinate.Coordinate, a.Coordinate.Label}, a.MaterialID, a.Availability, a.UnavailableReason}
}
func FromProvenance(p queries.Provenance) Provenance {
	return Provenance{p.MessageID, FromPrincipal(p.SubmittedBy)}
}
func FromInboxMessage(m queries.InboxMessage) InboxMessage {
	result := InboxMessage{InboxMetadata: InboxMetadata{m.ID, m.Source, m.Scope, m.Kind, m.Status, m.Sender, m.OccurredAt, m.ReceivedAt, m.Revision}, MessageID: m.MessageID, Text: m.Text, SourceURL: m.SourceURL, Artifacts: []MaterialAssociation{}, SubmittedBy: FromPrincipal(m.SubmittedBy)}
	for _, a := range m.Artifacts {
		result.Artifacts = append(result.Artifacts, FromMaterialAssociation(a))
	}
	if m.ProposedTaskRef != nil {
		result.ProposedTaskRef = &TaskRef{m.ProposedTaskRef.TaskID, m.ProposedTaskRef.Revision}
	}
	if m.Resolution != nil {
		r := m.Resolution
		result.Resolution = &Resolution{r.ID, r.Action, r.Reason, r.TaskID, FromPrincipal(r.AppliedBy), r.AppliedAt}
	}
	return result
}
func FromTask(t queries.Task) Task {
	result := Task{TaskSummary: TaskSummary{t.ID, t.Kind, t.Status, t.Title, t.Summary, t.NextAction, t.NextActor, t.Revision, t.UpdatedAt}, CreatedAt: t.CreatedAt, Timeline: []TaskEvent{}, Materials: []MaterialAssociation{}}
	if t.Provenance != nil {
		p := FromProvenance(*t.Provenance)
		result.Provenance = &p
	}
	for _, e := range t.Timeline {
		result.Timeline = append(result.Timeline, TaskEvent{e.ID, e.Kind, e.At, e.Reason, FromProvenance(e.Provenance), FromPrincipal(e.AppliedBy)})
	}
	for _, a := range t.Materials {
		result.Materials = append(result.Materials, FromMaterialAssociation(a))
	}
	return result
}
func FromTaskDetails(d queries.TaskDetails) TaskDetails {
	return TaskDetails{d.WorkspaceRevision, FromTask(d.Task)}
}
func FromInboxDetails(d queries.InboxDetails) InboxDetails {
	return InboxDetails{d.WorkspaceRevision, FromInboxMessage(d.Message)}
}
func FromMaterialResult(r queries.MaterialResult) MaterialResult {
	m := r.Material
	return MaterialResult{r.WorkspaceRevision, Material{m.ID, m.Availability, m.MediaType, m.Text, m.UnavailableReason}}
}
