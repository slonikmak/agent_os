// Package tasks owns pure task construction rules, independent of storage and transports.
package tasks

import (
	"strings"
	"unicode/utf8"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
)

type NewTask struct {
	Kind, Title                    string
	Summary, NextAction, NextActor *string
}

func ValidNew(input NewTask) bool {
	if input.Kind != "request" || strings.TrimSpace(input.Title) == "" || !utf8.ValidString(input.Title) || utf8.RuneCountInString(input.Title) > 256 {
		return false
	}
	for _, field := range []struct {
		value *string
		limit int
	}{{input.Summary, 4096}, {input.NextAction, 2048}, {input.NextActor, 256}} {
		if field.value != nil && (strings.TrimSpace(*field.value) == "" || !utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) > field.limit) {
			return false
		}
	}
	return true
}

// FromInbox retains the original submitter separately from the applying caller.
// Associations are copied as saved metadata; construction grants no file authority.
func FromInbox(id, eventID, at, reason string, input NewTask, message queries.InboxMessage, caller access.Principal) (queries.Task, error) {
	if !ValidNew(input) {
		return queries.Task{}, failure.New("invalid_request")
	}
	provenance := queries.Provenance{MessageID: message.ID, SubmittedBy: message.SubmittedBy}
	return queries.Task{
		TaskSummary: queries.TaskSummary{ID: id, Kind: input.Kind, Status: "new", Title: input.Title, Summary: input.Summary, NextAction: input.NextAction, NextActor: input.NextActor, Revision: 1, UpdatedAt: at},
		CreatedAt:   at, Provenance: &provenance,
		Timeline:  []queries.TaskEvent{{ID: eventID, Kind: "task.created", At: at, Reason: reason, Provenance: provenance, AppliedBy: caller}},
		Materials: append([]queries.MaterialAssociation{}, message.Artifacts...),
	}, nil
}
