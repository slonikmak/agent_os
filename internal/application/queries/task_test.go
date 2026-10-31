package queries

import (
	"context"
	"errors"
	"personaldesk/internal/application/failure"
	"testing"
)

type taskStoreStub struct {
	storeFunc
	read func(context.Context, string) (TaskDetails, error)
}

func (s taskStoreStub) LoadTaskDetails(ctx context.Context, id string) (TaskDetails, error) {
	return s.read(ctx, id)
}
func TestTaskQueryValidationSafeFailuresAndCancellation(t *testing.T) {
	calls := 0
	store := taskStoreStub{read: func(context.Context, string) (TaskDetails, error) {
		calls++
		return TaskDetails{WorkspaceRevision: 3, Task: Task{TaskSummary: TaskSummary{ID: "T-one"}}}, nil
	}}
	service := New(store)
	for _, id := range []string{"", "../task", "bad id", "_task"} {
		_, err := service.GetTaskDetails(context.Background(), id)
		var f *Failure
		if !errors.As(err, &f) || f.Code != "invalid_request" {
			t.Fatal(id, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.GetTaskDetails(ctx, "T-one")
	var f *Failure
	if calls != 0 || !errors.As(err, &f) || f.Code != "service_unavailable" {
		t.Fatal(calls, err)
	}
	if got, err := service.GetTaskDetails(context.Background(), "T-one"); err != nil || got.Task.ID != "T-one" {
		t.Fatal(got, err)
	}
	for _, cause := range []error{errors.New("PRIVATE SQL PATH"), context.Canceled, context.DeadlineExceeded, failure.New("not_found"), &Failure{Code: "unknown", Message: "PRIVATE"}} {
		store.read = func(context.Context, string) (TaskDetails, error) { return TaskDetails{}, cause }
		_, err = New(store).GetTaskDetails(context.Background(), "T-one")
		if !errors.As(err, &f) || f.Message == "PRIVATE" {
			t.Fatal("unsafe query failure", err)
		}
	}
	for _, result := range []TaskDetails{{WorkspaceRevision: 0}, {WorkspaceRevision: MaxRevision + 1}, {WorkspaceRevision: 1, Task: Task{TaskSummary: TaskSummary{ID: "T-other"}}}} {
		store.read = func(context.Context, string) (TaskDetails, error) { return result, nil }
		if _, err := New(store).GetTaskDetails(context.Background(), "T-one"); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
}
