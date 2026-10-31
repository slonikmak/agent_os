// Package contractfixture supplies isolated, transport-independent E1 facts.
package contractfixture

import (
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/queries"
	"strings"
)

const At = "2026-10-29T00:00:00Z"

func Submitter() access.Principal {
	return access.Principal{ClientID: "client-" + strings.Repeat("1", 32), Role: access.Worker}
}
func Coordinator() access.Principal {
	return access.Principal{ClientID: "client-" + strings.Repeat("2", 32), Role: access.Coordinator}
}
func Association() queries.MaterialAssociation {
	reason := "not_imported"
	return queries.MaterialAssociation{Coordinate: queries.ArtifactCoordinate{Kind: "file", Coordinate: "synthetic/report.md"}, Availability: "unavailable", UnavailableReason: &reason}
}
func Message() queries.InboxMessage {
	return queries.InboxMessage{InboxMetadata: queries.InboxMetadata{ID: "I-0001", Source: "manual", Scope: "personal", Kind: "user_request", Status: "pending", ReceivedAt: At, Revision: 1}, MessageID: "request-001", Text: "Clarify the export behavior", Artifacts: []queries.MaterialAssociation{Association()}, SubmittedBy: Submitter()}
}
func Summary() queries.TaskSummary {
	return queries.TaskSummary{ID: "T-0001", Kind: "request", Status: "new", Title: "Clarify export behavior", Revision: 1, UpdatedAt: At}
}
func Task() queries.Task {
	provenance := queries.Provenance{MessageID: "I-0001", SubmittedBy: Submitter()}
	return queries.Task{TaskSummary: Summary(), CreatedAt: At, Provenance: &provenance, Timeline: []queries.TaskEvent{{ID: "E-0001", Kind: "task.created", At: At, Reason: "This request introduces a new obligation", Provenance: provenance, AppliedBy: Coordinator()}}, Materials: []queries.MaterialAssociation{Association()}}
}
func AppliedMessage() queries.InboxMessage {
	m := Message()
	m.Status = "applied"
	m.Revision = 2
	m.Resolution = &queries.Resolution{ID: "R-0001", Action: "createTask", Reason: "This request introduces a new obligation", TaskID: "T-0001", AppliedBy: Coordinator(), AppliedAt: At}
	return m
}
func Snapshot() queries.WorkspaceSnapshot {
	return queries.WorkspaceSnapshot{WorkspaceID: strings.Repeat("0", 31) + "1", WorkspaceRevision: 3, CreatedAt: At, Tasks: []queries.TaskSummary{Summary()}, Inbox: []queries.InboxMetadata{Message().InboxMetadata}, Sources: []queries.SourceHealth{{Source: "manual", Scope: "personal", Status: "not_configured"}}, Counts: queries.Counts{Tasks: 1, Inbox: 1, UnprocessedInbox: 1}}
}
