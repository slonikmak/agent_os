package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
	"personaldesk/internal/application/tasks"
)

const CreateTaskOperation = "inbox.resolve.createTask"

type CreateTaskReceipt struct {
	RequestHash string
	Status      int
	Result      CreateTaskResult
}
type CreateTaskTx interface {
	FindCreateTaskReceipt(context.Context, string, string, string) (CreateTaskReceipt, bool, error)
	GetMessage(context.Context, string) (queries.InboxMessage, error)
	AllocateCreation(context.Context) (taskID, eventID, resolutionID string, err error)
	InsertTask(context.Context, queries.Task) error
	ResolveMessage(context.Context, queries.InboxMessage, string) error
	Revision(context.Context, bool) (int64, error)
	InsertCreateTaskReceipt(context.Context, string, string, string, CreateTaskReceipt) error
}
type CreateTaskWork interface {
	WithinCreateTask(context.Context, func(CreateTaskTx) error) error
}
type TaskCreator interface {
	CreateTask(context.Context, string, string, CreateTask) (CreateTaskResult, int, error)
}
type CreateTaskService struct {
	work    CreateTaskWork
	now     func() time.Time
	changed func(int64)
}

func NewTaskCreator(work CreateTaskWork, changed func(int64)) *CreateTaskService {
	return &CreateTaskService{work: work, now: time.Now, changed: changed}
}
func createTaskHash(id string, input CreateTask) (string, error) {
	data, err := json.Marshal(struct {
		Version                 int
		Operation, Target       string
		ExpectedMessageRevision int64
		Action, Reason          string
		Task                    NewTask
	}{1, CreateTaskOperation, "/api/v1/inbox/messages/" + id + "/resolutions", input.ExpectedMessageRevision, input.Action, input.Reason, input.Task})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (s *CreateTaskService) CreateTask(ctx context.Context, id, key string, input CreateTask) (result CreateTaskResult, status int, err error) {
	defer func() {
		if recover() != nil {
			result = CreateTaskResult{}
			status = 0
			err = failure.RecoveredPanic()
		}
	}()
	if !access.Allowed(input.AppliedBy, access.ResolveCreateTask) {
		return result, 0, failure.New("forbidden")
	}
	if !queries.ValidID(id) || !validKey(key) || input.ExpectedMessageRevision < 1 || input.ExpectedMessageRevision > queries.MaxRevision || input.Action != "createTask" || strings.TrimSpace(input.Reason) == "" || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 4096 || !tasks.ValidNew(input.Task) {
		return result, 0, failure.New("invalid_request")
	}
	hash, e := createTaskHash(id, input)
	if e != nil {
		return result, 0, failure.New("internal_error")
	}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	fresh := false
	err = s.work.WithinCreateTask(ctx, func(tx CreateTaskTx) error {
		receipt, found, e := tx.FindCreateTaskReceipt(ctx, input.AppliedBy.ClientID, CreateTaskOperation, key)
		if e != nil {
			return e
		}
		if found {
			if receipt.RequestHash != hash {
				return failure.New("idempotency_conflict")
			}
			result, status = receipt.Result, receipt.Status
			return nil
		}
		message, e := tx.GetMessage(ctx, id)
		if e != nil {
			return e
		}
		// A different-key terminal command wins over its obsolete expected revision.
		if message.Status == "applied" || message.Status == "ignored" || message.Resolution != nil {
			return failure.New("terminal_resolution")
		}
		if message.Revision != input.ExpectedMessageRevision {
			f := failure.New("revision_conflict")
			f.CurrentRevision = &message.Revision
			return f
		}
		if message.Status != "pending" || (message.Kind != "user_request" && message.Kind != "external_signal") || message.ProposedTaskRef != nil {
			return failure.New("unsupported_operation")
		}
		if message.Revision == queries.MaxRevision {
			return failure.New("service_unavailable")
		}
		taskID, eventID, resolutionID, e := tx.AllocateCreation(ctx)
		if e != nil {
			return e
		}
		at := s.now().UTC().Format(time.RFC3339Nano)
		task, e := tasks.FromInbox(taskID, eventID, at, input.Reason, input.Task, message, input.AppliedBy)
		if e != nil {
			return e
		}
		if e = tx.InsertTask(ctx, task); e != nil {
			return e
		}
		message.Status = "applied"
		message.Revision++
		message.Resolution = &queries.Resolution{ID: resolutionID, Action: input.Action, Reason: input.Reason, TaskID: task.ID, AppliedBy: input.AppliedBy, AppliedAt: at}
		if e = tx.ResolveMessage(ctx, message, hash); e != nil {
			return e
		}
		revision, e := tx.Revision(ctx, true)
		if e != nil {
			return e
		}
		result, status = CreateTaskResult{revision, message, task}, 201
		if e = tx.InsertCreateTaskReceipt(ctx, input.AppliedBy.ClientID, CreateTaskOperation, key, CreateTaskReceipt{hash, status, result}); e != nil {
			return e
		}
		fresh = true
		return nil
	})
	if err != nil {
		var f *failure.Failure
		if errors.As(err, &f) {
			return CreateTaskResult{}, 0, failure.Safe(f)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CreateTaskResult{}, 0, failure.New("service_unavailable")
		}
		return CreateTaskResult{}, 0, failure.New("internal_error")
	}
	if fresh {
		func() {
			defer func() { _ = recover() }()
			if s.changed != nil {
				s.changed(result.WorkspaceRevision)
			}
		}()
	}
	return result, status, nil
}
