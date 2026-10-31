package intake

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
)

type createMemory struct {
	message              queries.InboxMessage
	receipt              CreateTaskReceipt
	reads, writes, calls int
}

func (m *createMemory) WithinCreateTask(ctx context.Context, fn func(CreateTaskTx) error) error {
	m.calls++
	return fn(m)
}
func (m *createMemory) FindCreateTaskReceipt(context.Context, string, string, string) (CreateTaskReceipt, bool, error) {
	return m.receipt, m.receipt.Status != 0, nil
}
func (m *createMemory) GetMessage(context.Context, string) (queries.InboxMessage, error) {
	m.reads++
	return m.message, nil
}
func (m *createMemory) AllocateCreation(context.Context) (string, string, string, error) {
	return "T-one", "E-one", "R-one", nil
}
func (m *createMemory) InsertTask(context.Context, queries.Task) error { m.writes++; return nil }
func (m *createMemory) ResolveMessage(_ context.Context, q queries.InboxMessage, _ string) error {
	m.message = q
	m.writes++
	return nil
}
func (m *createMemory) Revision(context.Context, bool) (int64, error) { return 3, nil }
func (m *createMemory) InsertCreateTaskReceipt(_ context.Context, _, _, _ string, r CreateTaskReceipt) error {
	m.receipt = r
	m.writes++
	return nil
}
func pureCreate() CreateTask {
	return CreateTask{ExpectedMessageRevision: 1, Action: "createTask", Reason: "A new obligation", Task: NewTask{Kind: "request", Title: "Saved task"}, AppliedBy: access.Principal{ClientID: "client-" + strings.Repeat("2", 32), Role: access.Coordinator}}
}
func pureMessage() queries.InboxMessage {
	return queries.InboxMessage{InboxMetadata: queries.InboxMetadata{ID: "I-one", Status: "pending", Kind: "user_request", Revision: 1}, SubmittedBy: access.Principal{ClientID: "client-" + strings.Repeat("1", 32), Role: access.Worker}, Artifacts: []queries.MaterialAssociation{}}
}
func pureCode(t *testing.T, err error, code string) {
	t.Helper()
	var f *failure.Failure
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("want %s: %v", code, err)
	}
}
func TestCreateWorkflowPureConstructionAndReplayBeforeEntityRead(t *testing.T) {
	m := &createMemory{message: pureMessage()}
	events := 0
	s := NewTaskCreator(m, func(int64) { events++ })
	s.now = func() time.Time { return time.Date(2026, 10, 29, 14, 0, 0, 0, time.FixedZone("test", 7200)) }
	first, status, err := s.CreateTask(context.Background(), "I-one", "key", pureCreate())
	if err != nil || status != 201 || first.Task.CreatedAt != "2026-10-29T12:00:00Z" || first.Task.Status != "new" || first.Task.Revision != 1 || first.Task.Provenance.SubmittedBy != pureMessage().SubmittedBy || first.Task.Timeline[0].AppliedBy != pureCreate().AppliedBy || first.Message.Status != "applied" || first.Message.Revision != 2 || first.Message.Resolution.TaskID != first.Task.ID || first.Task.Summary != nil {
		t.Fatalf("%+v %d %v", first, status, err)
	}
	m.message = queries.InboxMessage{}
	replay, status, err := s.CreateTask(context.Background(), "I-one", "key", pureCreate())
	if err != nil || status != 201 || !reflect.DeepEqual(first, replay) || m.reads != 1 || m.writes != 3 || events != 1 {
		t.Fatal("replay read mutable state or republished", err)
	}
	_, _, err = s.CreateTask(context.Background(), "I-other", "key", pureCreate())
	pureCode(t, err, "idempotency_conflict")
	changed := pureCreate()
	changed.Reason = "Other reason"
	_, _, err = s.CreateTask(context.Background(), "I-one", "key", changed)
	pureCode(t, err, "idempotency_conflict")
}
func TestCreateWorkflowPureAuthorizationAndConflictPrecedence(t *testing.T) {
	for _, role := range []access.Role{access.Reader, access.Worker} {
		m := &createMemory{}
		in := pureCreate()
		in.AppliedBy.Role = role
		_, _, err := NewTaskCreator(m, nil).CreateTask(context.Background(), "I-one", "key", in)
		pureCode(t, err, "forbidden")
		if m.calls != 0 {
			t.Fatal("authorization reached work")
		}
	}
	for _, state := range []string{"applied", "ignored", "stale", "needs_attention", "agent_report", "association"} {
		t.Run(state, func(t *testing.T) {
			m := &createMemory{message: pureMessage()}
			code := "unsupported_operation"
			switch state {
			case "applied", "ignored":
				m.message.Status = state
				m.message.Revision = 2
				code = "terminal_resolution"
			case "stale":
				m.message.Revision = 2
				code = "revision_conflict"
			case "needs_attention":
				m.message.Status = state
			case "agent_report":
				m.message.Kind = state
			case "association":
				m.message.ProposedTaskRef = &queries.TaskRef{TaskID: "T-proposed", Revision: 1}
			}
			_, _, err := NewTaskCreator(m, nil).CreateTask(context.Background(), "I-one", "key", pureCreate())
			pureCode(t, err, code)
			if m.writes != 0 {
				t.Fatal("rejected command wrote")
			}
		})
	}
}
