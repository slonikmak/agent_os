package wailsread

import (
	"context"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
	"time"
)

type TaskResult struct {
	Data  *TaskDetails `json:"data"`
	Error *Failure     `json:"error"`
}

func (f *Facade) GetTaskDetails(ctx context.Context, id string) (result TaskResult) {
	requestID := diagnostics.ID()
	defer f.diag.Operation("wailsread", "task.read", requestID, time.Now())
	fault := func() TaskResult {
		return TaskResult{Error: f.faultOperation(requestID, "task.read", "internal").Error}
	}
	defer func() {
		if recover() != nil {
			result = TaskResult{Error: f.faultOperation(requestID, "task.read", "panic").Error}
		}
	}()
	reader, ok := f.reader.(queries.TaskReader)
	if !ok || f.diag.Status().RestartRequired {
		return TaskResult{Error: publicFailure(failure.New("service_unavailable"), requestID, nil)}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	detail, err := reader.GetTaskDetails(ctx, id)
	if err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return TaskResult{Error: safe}
		}
		return fault()
	}
	view := FromTaskDetails(detail)
	if err := validateResponse("task-details", view); err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return TaskResult{Error: safe}
		}
		return fault()
	}
	return TaskResult{Data: &view}
}
