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
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
)

func createInput() intake.CreateTask {
	return intake.CreateTask{ExpectedMessageRevision: 1, Action: "createTask", Reason: "This request introduces an obligation", Task: intake.NewTask{Kind: "request", Title: "Atomic saved task"}, AppliedBy: access.Principal{ClientID: "client-" + strings.Repeat("2", 32), Role: access.Coordinator}}
}
func seededCreate(t *testing.T) (*Store, string, intake.ReceiveResult) {
	t.Helper()
	s, path := intakeStore(t)
	r, _, err := intake.NewReceiver(s, nil).ReceiveMessage(context.Background(), "receive", receiveInput())
	if err != nil {
		t.Fatal(err)
	}
	return s, path, r
}

// Compare all committed command facts before resetting fault fixtures. Counts
// alone would miss an altered original, receipt JSON, counter or entity revision.
func creationFacts(t *testing.T, s *Store) string {
	t.Helper()
	all := []any{}
	for _, table := range []string{"workspace_meta", "workspace_counters", "inbox_messages", "inbox_payloads", "tasks", "task_creation", "task_events", "inbox_resolutions", "mutation_receipts"} {
		rows, err := s.writer.Query("SELECT * FROM " + table + " ORDER BY 1,2")
		if err != nil {
			t.Fatal(err)
		}
		columns, _ := rows.Columns()
		saved := [][]any{}
		for rows.Next() {
			v := make([]any, len(columns))
			dest := make([]any, len(v))
			for i := range v {
				dest[i] = &v[i]
			}
			if err = rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			saved = append(saved, v)
		}
		if err = closeRows(rows); err != nil {
			t.Fatal(err)
		}
		all = append(all, saved)
	}
	data, _ := json.Marshal(all)
	return string(data)
}
func assertCreatedCounts(t *testing.T, s *Store, receipts int) {
	t.Helper()
	var tasks, creation, events, resolutions, gotReceipts int
	var a, b, c, revision int64
	err := s.writer.QueryRow(`SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM task_creation),(SELECT count(*) FROM task_events),(SELECT count(*) FROM inbox_resolutions),(SELECT count(*) FROM mutation_receipts),task_sequence,event_sequence,resolution_sequence,(SELECT workspace_revision FROM workspace_meta) FROM workspace_counters`).Scan(&tasks, &creation, &events, &resolutions, &gotReceipts, &a, &b, &c, &revision)
	if err != nil || tasks != 1 || creation != 1 || events != 1 || resolutions != 1 || gotReceipts != receipts || a != 1 || b != 1 || c != 1 || revision != 3 {
		t.Fatalf("partial creation: %d %d %d %d receipts=%d counters=%d,%d,%d revision=%d err=%v", tasks, creation, events, resolutions, gotReceipts, a, b, c, revision, err)
	}
}
func TestCreateAtomicSavedFactsReceiveRedeliveryAndRestartReplay(t *testing.T) {
	ctx := context.Background()
	s, path, received := seededCreate(t)
	var publications atomic.Int32
	creator := intake.NewTaskCreator(s, func(int64) { publications.Add(1) })
	first, status, err := creator.CreateTask(ctx, received.Message.ID, "create", createInput())
	if err != nil || status != 201 || first.WorkspaceRevision != 3 || first.Message.Status != "applied" || first.Message.Revision != 2 || first.Task.Provenance.MessageID != received.Message.ID || first.Task.Provenance.SubmittedBy != received.Message.SubmittedBy || first.Task.Timeline[0].AppliedBy != createInput().AppliedBy || first.Task.Timeline[0].Reason != createInput().Reason || !reflect.DeepEqual(first.Task.Materials, received.Message.Artifacts) {
		t.Fatalf("%+v %d %v", first, status, err)
	}
	if _, err = time.Parse(time.RFC3339Nano, first.Task.CreatedAt); err != nil || !strings.HasSuffix(first.Task.CreatedAt, "Z") || first.Task.CreatedAt != first.Task.UpdatedAt || first.Task.CreatedAt != first.Message.Resolution.AppliedAt {
		t.Fatal("invalid genuine UTC creation")
	}
	if first.Task.Summary != nil || first.Task.NextAction != nil || first.Task.NextActor != nil || first.Task.Materials[0].MaterialID != nil {
		t.Fatal("invented fields or material authority")
	}
	detail, err := s.LoadInboxMessage(ctx, received.Message.ID)
	if err != nil || !reflect.DeepEqual(detail.Message, first.Message) {
		t.Fatal("saved resolution drift", err)
	}
	snapshot, err := s.LoadWorkspace(ctx)
	if err != nil || !reflect.DeepEqual(snapshot.Tasks[0], first.Task.TaskSummary) || snapshot.Counts.Tasks != 1 || snapshot.Counts.UnprocessedInbox != 0 || snapshot.Inbox[0].Status != "applied" {
		t.Fatal("saved card/count drift", err)
	}
	before := creationFacts(t, s)
	_, _, err = creator.CreateTask(ctx, received.Message.ID, "different", createInput())
	requireCode(t, err, "terminal_resolution")
	_, _, err = creator.CreateTask(ctx, "I-missing", "create", createInput())
	requireCode(t, err, "idempotency_conflict")
	otherCaller := createInput()
	otherCaller.AppliedBy.ClientID = "client-" + strings.Repeat("3", 32)
	_, _, err = creator.CreateTask(ctx, received.Message.ID, "create", otherCaller)
	requireCode(t, err, "terminal_resolution")
	if before != creationFacts(t, s) {
		t.Fatal("conflicts changed committed facts")
	}
	receiver := intake.NewReceiver(s, nil)
	original, status, err := receiver.ReceiveMessage(ctx, "receive", receiveInput())
	if err != nil || status != 201 || !reflect.DeepEqual(original, received) {
		t.Fatal("receive receipt lost original pending result", err)
	}
	current, status, err := receiver.ReceiveMessage(ctx, "after-resolution", receiveInput())
	if err != nil || status != 200 || !reflect.DeepEqual(current.Message, first.Message) || current.WorkspaceRevision != 3 {
		t.Fatal("identity redelivery lost saved resolution", err)
	}
	assertCreatedCounts(t, s, 3)
	// Later workspace changes cannot rewrite this command's original revision.
	later := receiveInput()
	later.MessageID = "later-workspace-change"
	if _, _, err = receiver.ReceiveMessage(ctx, "later", later); err != nil {
		t.Fatal(err)
	}
	before = creationFacts(t, s)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	replay, status, err := intake.NewTaskCreator(s, nil).CreateTask(ctx, received.Message.ID, "create", createInput())
	if err != nil || status != 201 || !reflect.DeepEqual(replay, first) || before != creationFacts(t, s) || publications.Load() != 1 {
		t.Fatal("restart receipt/effect drift", err)
	}
	var createdAt, materials, reason, submitter, applying string
	if err = s.writer.QueryRow(`SELECT c.created_at,c.materials_json,e.reason,e.submitted_client_id,e.applied_client_id FROM task_creation c JOIN task_events e ON e.task_id=c.task_id`).Scan(&createdAt, &materials, &reason, &submitter, &applying); err != nil || createdAt != first.Task.CreatedAt || reason != createInput().Reason || submitter != received.Message.SubmittedBy.ClientID || applying != createInput().AppliedBy.ClientID {
		t.Fatal("provenance/history not durable", err)
	}
	var savedMaterials []queries.MaterialAssociation
	if err = json.Unmarshal([]byte(materials), &savedMaterials); err != nil || !reflect.DeepEqual(savedMaterials, first.Task.Materials) {
		t.Fatal("task associations not durable", err)
	}
}
func TestCreateConcurrentSameAndDifferentKeysHaveOneOutcome(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			s, _, received := seededCreate(t)
			var published atomic.Int32
			creator := intake.NewTaskCreator(s, func(int64) { published.Add(1) })
			var group sync.WaitGroup
			var successes atomic.Int32
			var terminal atomic.Int32
			for n := range 20 {
				group.Add(1)
				go func(n int) {
					defer group.Done()
					key := "same"
					if !same {
						key = fmt.Sprintf("key-%d", n)
					}
					_, status, err := creator.CreateTask(context.Background(), received.Message.ID, key, createInput())
					if err != nil {
						var f *queries.Failure
						if errors.As(err, &f) && f.Code == "terminal_resolution" {
							terminal.Add(1)
						} else {
							t.Error(err)
						}
					} else if status == 201 {
						successes.Add(1)
					}
				}(n)
			}
			group.Wait()
			if published.Load() != 1 || (same && successes.Load() != 20) || (!same && (successes.Load() != 1 || terminal.Load() != 19)) {
				t.Fatal("competing create outcomes", published.Load(), successes.Load(), terminal.Load())
			}
			assertCreatedCounts(t, s, 2)
		})
	}
}
func TestCreateRollbackFaultPanicCancellationExhaustionAndSharedGate(t *testing.T) {
	for _, fault := range []string{"fault", "panic", "cancel", "task_sequence", "event_sequence", "resolution_sequence", "message_revision", "workspace_revision"} {
		t.Run(fault, func(t *testing.T) {
			s, _, received := seededCreate(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			published := 0
			in := createInput()
			switch fault {
			case "fault":
				s.beforeCommit = func() error { return errors.New("PRIVATE SQL BODY") }
			case "panic":
				s.beforeCommit = func() error { panic("PRIVATE SQL BODY") }
			case "cancel":
				s.beforeCommit = func() error { cancel(); return nil }
			case "task_sequence", "event_sequence", "resolution_sequence":
				_, _ = s.writer.Exec("UPDATE workspace_counters SET "+fault+"=?", queries.MaxRevision)
			case "message_revision":
				_, _ = s.writer.Exec(`UPDATE inbox_messages SET revision=?`, queries.MaxRevision)
				in.ExpectedMessageRevision = queries.MaxRevision
			case "workspace_revision":
				_, _ = s.writer.Exec(`UPDATE workspace_meta SET workspace_revision=?`, queries.MaxRevision)
			}
			before := creationFacts(t, s)
			_, _, err := intake.NewTaskCreator(s, func(int64) { published++ }).CreateTask(ctx, received.Message.ID, "create", in)
			if fault == "fault" || fault == "panic" {
				requireCode(t, err, "internal_error")
			} else {
				requireCode(t, err, "service_unavailable")
			}
			if before != creationFacts(t, s) || published != 0 {
				t.Fatal("partial facts survived rollback")
			}
			s.beforeCommit = nil
			_, _ = s.writer.Exec(`UPDATE workspace_counters SET task_sequence=0,event_sequence=0,resolution_sequence=0`)
			_, _ = s.writer.Exec(`UPDATE workspace_meta SET workspace_revision=2`)
			_, _ = s.writer.Exec(`UPDATE inbox_messages SET revision=1`)
			if _, _, err = intake.NewTaskCreator(s, nil).CreateTask(context.Background(), received.Message.ID, "create", createInput()); err != nil {
				t.Fatal("gate not released", err)
			}
			assertCreatedCounts(t, s, 2)
		})
	}
	s, _, received := seededCreate(t)
	before := creationFacts(t, s)
	s.writeGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := intake.NewTaskCreator(s, nil).CreateTask(ctx, received.Message.ID, "occupied", createInput())
	requireCode(t, err, "service_unavailable")
	<-s.writeGate
	if before != creationFacts(t, s) {
		t.Fatal("occupied gate changed state")
	}
}
func TestCreateStateRefusalsAndOperationScope(t *testing.T) {
	for _, state := range []string{"missing", "stale", "agent_report", "association", "needs_attention"} {
		t.Run(state, func(t *testing.T) {
			s, _, received := seededCreate(t)
			id := received.Message.ID
			code := "unsupported_operation"
			switch state {
			case "missing":
				id = "I-missing"
				code = "not_found"
			case "stale":
				_, _ = s.writer.Exec(`UPDATE inbox_messages SET revision=2`)
				code = "revision_conflict"
			case "agent_report":
				_, _ = s.writer.Exec(`UPDATE inbox_messages SET kind='agent_report'`)
			case "association":
				_, _ = s.writer.Exec(`UPDATE inbox_payloads SET proposed_task_json='{"TaskID":"T-proposed","Revision":1}'`)
			case "needs_attention":
				_, _ = s.writer.Exec(`UPDATE inbox_messages SET status='needs_attention'`)
			}
			before := creationFacts(t, s)
			_, _, err := intake.NewTaskCreator(s, nil).CreateTask(context.Background(), id, "create", createInput())
			requireCode(t, err, code)
			if code == "revision_conflict" {
				var f *queries.Failure
				_ = errors.As(err, &f)
				if f.CurrentRevision == nil || *f.CurrentRevision != 2 {
					t.Fatal("missing current revision")
				}
			}
			if before != creationFacts(t, s) {
				t.Fatal("refusal wrote facts")
			}
		})
	}
	s, _ := intakeStore(t)
	in := receiveInput()
	in.SubmittedBy = createInput().AppliedBy
	received, _, err := intake.NewReceiver(s, nil).ReceiveMessage(context.Background(), "shared-key", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = intake.NewTaskCreator(s, nil).CreateTask(context.Background(), received.Message.ID, "shared-key", createInput()); err != nil {
		t.Fatal("receive/create key scopes collide", err)
	}
	assertCreatedCounts(t, s, 2)
}
func TestCreatePublicationFailureAndTerminalConstraintPermitNonterminalHistory(t *testing.T) {
	s, _, received := seededCreate(t)
	creator := intake.NewTaskCreator(s, func(int64) { panic("PRIVATE sink") })
	first, status, err := creator.CreateTask(context.Background(), received.Message.ID, "key", createInput())
	if err != nil || status != 201 {
		t.Fatal(status, err)
	}
	replay, _, err := creator.CreateTask(context.Background(), received.Message.ID, "key", createInput())
	if err != nil || !reflect.DeepEqual(replay, first) {
		t.Fatal("notification changed success")
	}
	for _, id := range []string{"R-history1", "R-history2"} {
		if _, err = s.writer.Exec(`INSERT INTO inbox_resolutions(id,message_id,action,reason,applied_client_id,applied_role,applied_at,request_hash,terminal) VALUES(?,?,'needs_attention','history',?,'coordinator',?,'hash',0)`, id, received.Message.ID, createInput().AppliedBy.ClientID, first.Task.CreatedAt); err != nil {
			t.Fatal("nonterminal history prohibited", err)
		}
	}
	_, err = s.writer.Exec(`INSERT INTO inbox_resolutions SELECT 'R-duplicate',message_id,action,reason,task_id,applied_client_id,applied_role,applied_at,request_hash,terminal FROM inbox_resolutions WHERE terminal=1`)
	if err == nil {
		t.Fatal("second terminal resolution accepted")
	}
	for _, query := range []string{`UPDATE workspace_counters SET task_sequence=-1`, `UPDATE workspace_counters SET event_sequence=9007199254740992`, `INSERT INTO task_creation VALUES('T-missing','time','I-missing','client','worker','[]')`, `UPDATE task_events SET applied_role='worker'`} {
		if _, err = s.writer.Exec(query); err == nil {
			t.Fatal("constraint missing", query)
		}
	}
}
