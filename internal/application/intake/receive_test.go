package intake

import (
	"context"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/queries"
	"strings"
	"testing"
)

type noWork struct{ calls int }

func (w *noWork) Within(context.Context, func(ReceiveTx) error) error { w.calls++; return nil }
func TestReceiveAuthorizationAndKeyPrecedeTransaction(t *testing.T) {
	w := &noWork{}
	receiver := NewReceiver(w, nil)
	input := Receive{SubmittedBy: access.Principal{ClientID: "client-" + strings.Repeat("1", 32), Role: access.Reader}}
	if _, _, err := receiver.ReceiveMessage(context.Background(), "key", input); err == nil {
		t.Fatal("reader received")
	}
	input.SubmittedBy.Role = access.Worker
	for _, key := range []string{"", "bad key", ".bad", strings.Repeat("x", 129)} {
		if _, _, err := receiver.ReceiveMessage(context.Background(), key, input); err == nil {
			t.Fatal("bad key received")
		}
	}
	if w.calls != 0 {
		t.Fatal("untrusted request reached transaction")
	}
}
func TestCanonicalReceiveHashPreservesFactsAndExcludesSubmitter(t *testing.T) {
	input := Receive{Source: "manual", Scope: "personal", MessageID: "one", Text: " text\n", Artifacts: []queries.ArtifactCoordinate{{Kind: "reference", Coordinate: "one"}, {Kind: "reference", Coordinate: "two"}}}
	first, _ := receiveHash(input)
	input.SubmittedBy = access.Principal{ClientID: "other", Role: access.Worker}
	same, _ := receiveHash(input)
	if first != same {
		t.Fatal("submitter became immutable claimed content")
	}
	input.Text = "text\n"
	changed, _ := receiveHash(input)
	if changed == first {
		t.Fatal("hash trimmed original text")
	}
	input.Text = " text\n"
	input.Artifacts[0], input.Artifacts[1] = input.Artifacts[1], input.Artifacts[0]
	changed, _ = receiveHash(input)
	if changed == first {
		t.Fatal("hash sorted artifact order")
	}
	input.Artifacts = nil
	empty, _ := receiveHash(input)
	input.Artifacts = []queries.ArtifactCoordinate{}
	normalized, _ := receiveHash(input)
	if empty != normalized {
		t.Fatal("empty artifacts not normalized")
	}
	timeA, timeB := "2026-01-01T00:00:00Z", "2026-01-01T00:00:00.0Z"
	input.OccurredAt = &timeA
	a, _ := receiveHash(input)
	input.OccurredAt = &timeB
	b, _ := receiveHash(input)
	if a == b {
		t.Fatal("hash rewrote original timestamp")
	}
}
