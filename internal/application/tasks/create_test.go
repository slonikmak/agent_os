package tasks

import (
	"strings"
	"testing"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/queries"
)

func TestConstructRetainsNullableValuesAndInertAssociations(t *testing.T) {
	reason := "not_imported"
	m := queries.InboxMessage{InboxMetadata: queries.InboxMetadata{ID: "I-original"}, Artifacts: []queries.MaterialAssociation{{Coordinate: queries.ArtifactCoordinate{Kind: "file", Coordinate: "/private/inert"}, Availability: "unavailable", UnavailableReason: &reason}}}
	q, err := FromInbox("T-new", "E-new", "2026-10-29T00:00:00Z", "reason", NewTask{Kind: "request", Title: "new"}, m, access.Principal{})
	if err != nil || q.Summary != nil || q.NextAction != nil || q.NextActor != nil || q.Materials[0].MaterialID != nil || q.Timeline[0].Provenance.MessageID != m.ID {
		t.Fatal(q, err)
	}
	q.Materials[0].Availability = "changed"
	if m.Artifacts[0].Availability != "unavailable" {
		t.Fatal("aliased association list")
	}
}
func TestNewTaskRulesRejectInvalidOrUnsupportedValues(t *testing.T) {
	for _, input := range []NewTask{{Kind: "code_review", Title: "title"}, {Kind: "request", Title: " "}, {Kind: "request", Title: strings.Repeat("я", 257)}, {Kind: "request", Title: "title", NextActor: ptr(strings.Repeat("a", 257))}, {Kind: "request", Title: "title", Summary: ptr("")}} {
		if ValidNew(input) {
			t.Fatal("invalid task accepted")
		}
	}
}
func ptr(s string) *string { return &s }
