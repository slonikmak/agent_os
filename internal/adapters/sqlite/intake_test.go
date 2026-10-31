package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
	"personaldesk/internal/testsupport/migrationfixture"
	"personaldesk/migrations"
)

func receiveInput() intake.Receive {
	return intake.Receive{Source: "manual", Scope: "synthetic", MessageID: "request-1", Kind: "user_request", Text: "Original <script> text\n![inert](https://example.invalid/image)", Artifacts: []queries.ArtifactCoordinate{{Kind: "file", Coordinate: "/synthetic/private/path"}}, SubmittedBy: access.Principal{ClientID: "client-" + strings.Repeat("1", 32), Role: access.Worker}}
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var f *failure.Failure
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func intakeStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := database(t)
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}
func assertIntakeCounts(t *testing.T, s *Store, messages, receipts, sequence, revision int) {
	t.Helper()
	var m, r, c, v, tasks int
	if err := s.writer.QueryRow(`SELECT (SELECT count(*) FROM inbox_messages),(SELECT count(*) FROM mutation_receipts),(SELECT message_sequence FROM workspace_counters),(SELECT workspace_revision FROM workspace_meta),(SELECT count(*) FROM tasks)`).Scan(&m, &r, &c, &v, &tasks); err != nil {
		t.Fatal(err)
	}
	if m != messages || r != receipts || c != sequence || v != revision || tasks != 0 {
		t.Fatalf("partial effects: messages=%d receipts=%d counter=%d revision=%d tasks=%d", m, r, c, v, tasks)
	}
}
func TestReceiveImmutableReplayIdentityDuplicateAndRestart(t *testing.T) {
	ctx := context.Background()
	s, path := intakeStore(t)
	var published atomic.Int32
	receiver := intake.NewReceiver(s, func(int64) { published.Add(1) })
	input := receiveInput()
	first, status, err := receiver.ReceiveMessage(ctx, "first", input)
	if err != nil || status != 201 || first.WorkspaceRevision != 2 || first.Message.Revision != 1 || first.Message.Status != "pending" || first.Message.Text != input.Text || first.Message.SubmittedBy != input.SubmittedBy {
		t.Fatalf("first=%+v status=%d err=%v", first, status, err)
	}
	if _, err = time.Parse(time.RFC3339Nano, first.Message.ReceivedAt); err != nil || !strings.HasSuffix(first.Message.ReceivedAt, "Z") {
		t.Fatal("invalid server UTC receipt")
	}
	if first.Message.Artifacts[0].MaterialID != nil || *first.Message.Artifacts[0].UnavailableReason != "not_imported" {
		t.Fatal("coordinates gained file authority")
	}
	duplicate := input
	duplicate.SubmittedBy = access.Principal{ClientID: "client-" + strings.Repeat("2", 32), Role: access.Coordinator}
	same, status, err := receiver.ReceiveMessage(ctx, "second", duplicate)
	if err != nil || status != 200 || !reflect.DeepEqual(same, first) {
		t.Fatalf("identity duplicate changed first provenance: %+v %d %v", same, status, err)
	}
	changed := input
	changed.Text += " changed"
	_, _, err = receiver.ReceiveMessage(ctx, "first", changed)
	requireCode(t, err, "idempotency_conflict")
	_, _, err = receiver.ReceiveMessage(ctx, "third", changed)
	requireCode(t, err, "message_identity_conflict")
	assertIntakeCounts(t, s, 1, 2, 1, 2)
	detail, err := s.LoadInboxMessage(ctx, first.Message.ID)
	if err != nil || !reflect.DeepEqual(detail.Message, first.Message) || detail.WorkspaceRevision != 2 {
		t.Fatal("detail drift", err)
	}
	snapshot, err := s.LoadWorkspace(ctx)
	if err != nil || snapshot.Inbox[0].ID != first.Message.ID || snapshot.Counts.Inbox != 1 || snapshot.Counts.Tasks != 0 {
		t.Fatal("snapshot drift", err)
	}
	// Later state must never change the original committed success receipt.
	input.MessageID = "request-2"
	if _, _, err = receiver.ReceiveMessage(ctx, "fourth", input); err != nil {
		t.Fatal(err)
	}
	if _, err = s.writer.Exec(`UPDATE inbox_messages SET status='needs_attention',revision=2 WHERE id=?`, first.Message.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	receiver = intake.NewReceiver(s, nil)
	replay, status, err := receiver.ReceiveMessage(ctx, "first", receiveInput())
	if err != nil || status != 201 || !reflect.DeepEqual(replay, first) {
		t.Fatalf("restart replay drift: %+v %d %v", replay, status, err)
	}
	if published.Load() != 2 {
		t.Fatal("replay/duplicate published a domain change", published.Load())
	}
}
func TestConcurrentDeliveriesAndReceiptSequenceOrder(t *testing.T) {
	ctx := context.Background()
	s, _ := intakeStore(t)
	var events atomic.Int32
	receiver := intake.NewReceiver(s, func(int64) { events.Add(1) })
	var group sync.WaitGroup
	var fresh atomic.Int32
	for n := range 24 {
		group.Add(1)
		go func(n int) {
			defer group.Done()
			_, status, err := receiver.ReceiveMessage(ctx, fmt.Sprintf("key-%d", n), receiveInput())
			if err != nil {
				t.Errorf("delivery %d: %v", n, err)
			}
			if status == 201 {
				fresh.Add(1)
			}
		}(n)
	}
	group.Wait()
	assertIntakeCounts(t, s, 1, 24, 1, 2)
	if fresh.Load() != 1 || events.Load() != 1 {
		t.Fatal("duplicate effects", fresh.Load(), events.Load())
	}
	input := receiveInput()
	input.MessageID = "clock-backward"
	second, _, err := receiver.ReceiveMessage(ctx, "clock", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.writer.Exec(`UPDATE inbox_messages SET received_at=CASE WHEN id=? THEN '2020-01-01T00:00:00Z' ELSE '2030-01-01T00:00:00Z' END`, second.Message.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.LoadWorkspace(ctx)
	if err != nil || snapshot.Inbox[0].ID != second.Message.ID {
		t.Fatal("wall clock changed receipt ordering", err)
	}
}
func TestRollbackFaultCancellationExhaustionAndBoundedGate(t *testing.T) {
	for _, test := range []string{"fault", "cancel", "counter", "revision", "panic"} {
		t.Run(test, func(t *testing.T) {
			s, _ := intakeStore(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var events int
			expectedSequence, expectedRevision := int64(0), int64(1)
			receiver := intake.NewReceiver(s, func(int64) { events++ })
			switch test {
			case "fault":
				s.beforeCommit = func() error { return errors.New("PRIVATE SQL BODY") }
			case "panic":
				s.beforeCommit = func() error { panic("PRIVATE SQL BODY") }
			case "cancel":
				s.beforeCommit = func() error { cancel(); return nil }
			case "counter":
				expectedSequence = queries.MaxRevision
				_, _ = s.writer.Exec(`UPDATE workspace_counters SET message_sequence=?`, queries.MaxRevision)
			case "revision":
				expectedRevision = queries.MaxRevision
				_, _ = s.writer.Exec(`UPDATE workspace_meta SET workspace_revision=?`, queries.MaxRevision)
			}
			_, _, err := receiver.ReceiveMessage(ctx, "rollback", receiveInput())
			if test == "fault" || test == "panic" {
				requireCode(t, err, "internal_error")
			} else {
				requireCode(t, err, "service_unavailable")
			}
			if events != 0 {
				t.Fatal("rollback published")
			}
			var messages, receipts, payloads int
			var sequence, revision int64
			if err = s.writer.QueryRow(`SELECT (SELECT count(*) FROM inbox_messages),(SELECT count(*) FROM mutation_receipts),(SELECT count(*) FROM inbox_payloads),(SELECT message_sequence FROM workspace_counters),(SELECT workspace_revision FROM workspace_meta)`).Scan(&messages, &receipts, &payloads, &sequence, &revision); err != nil {
				t.Fatal(err)
			}
			if messages != 0 || receipts != 0 || payloads != 0 {
				t.Fatal("partial rollback")
			}
			if sequence != expectedSequence || revision != expectedRevision {
				t.Fatalf("rollback changed counter/revision: sequence=%d want=%d revision=%d want=%d", sequence, expectedSequence, revision, expectedRevision)
			}
			s.beforeCommit = nil
			_, _ = s.writer.Exec(`UPDATE workspace_counters SET message_sequence=0; UPDATE workspace_meta SET workspace_revision=1`)
			if _, _, err = receiver.ReceiveMessage(context.Background(), "rollback", receiveInput()); err != nil {
				t.Fatal("gate was not released", err)
			}
			assertIntakeCounts(t, s, 1, 1, 1, 2)
		})
	}
	s, _ := intakeStore(t)
	s.writeGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := intake.NewReceiver(s, nil).ReceiveMessage(ctx, "wait", receiveInput())
	requireCode(t, err, "service_unavailable")
	<-s.writeGate
	assertIntakeCounts(t, s, 0, 0, 0, 1)
}
func TestPostCommitPublicationPanicLeavesReplayableSuccess(t *testing.T) {
	s, _ := intakeStore(t)
	receiver := intake.NewReceiver(s, func(int64) { panic("sink failed") })
	first, status, err := receiver.ReceiveMessage(context.Background(), "key", receiveInput())
	if err != nil || status != 201 {
		t.Fatal(status, err)
	}
	replay, status, err := receiver.ReceiveMessage(context.Background(), "key", receiveInput())
	if err != nil || status != 201 || !reflect.DeepEqual(first, replay) {
		t.Fatal("commit reversed by publication failure")
	}
	assertIntakeCounts(t, s, 1, 1, 1, 2)
}
func TestActualV1UpgradePreservesUnknownOriginalAndReservesIdentity(t *testing.T) {
	path := database(t)
	if err := migrationfixture.Populate(path); err != nil {
		t.Fatal(err)
	}
	catalog, _ := migrations.Embedded()
	backup := database(t)
	s, err := OpenCatalog(context.Background(), path, catalog, func(ctx context.Context, state State) error {
		if state.Version != 1 {
			t.Fatal(state)
		}
		_, err := Backup(ctx, path, backup, catalog)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.LoadInboxMessage(context.Background(), "I-frozen")
	requireCode(t, err, "service_unavailable")
	input := receiveInput()
	input.Source = "manual"
	input.Scope = "isolated"
	input.MessageID = "fixture-1"
	_, _, err = intake.NewReceiver(s, nil).ReceiveMessage(context.Background(), "legacy", input)
	requireCode(t, err, "message_identity_conflict")
	snapshot, err := s.LoadWorkspace(context.Background())
	if err != nil || snapshot.WorkspaceRevision != 42 || snapshot.Inbox[0].Revision != 3 || snapshot.Tasks[0].Revision != 7 {
		t.Fatal("legacy facts altered", err)
	}
	state, err := InspectImage(context.Background(), backup, catalog)
	if err != nil || state.Version != 1 {
		t.Fatal("v1 recovery image lost", err)
	}
	var count int
	_ = s.writer.QueryRow(`SELECT count(*) FROM mutation_receipts`).Scan(&count)
	if count != 0 {
		t.Fatal("legacy conflict created receipt")
	}
	encoded, _ := json.Marshal(snapshot.Inbox)
	if strings.Contains(string(encoded), "Original") {
		t.Fatal("metadata contains body")
	}
}
