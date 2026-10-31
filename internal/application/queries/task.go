package queries

import (
	"context"
	"errors"
	"personaldesk/internal/application/failure"
)

type TaskStore interface {
	LoadTaskDetails(context.Context, string) (TaskDetails, error)
}
type TaskReader interface {
	GetTaskDetails(context.Context, string) (TaskDetails, error)
}

func (s *Service) GetTaskDetails(ctx context.Context, id string) (TaskDetails, error) {
	if !ValidID(id) {
		return TaskDetails{}, failure.New("invalid_request")
	}
	if ctx.Err() != nil {
		return TaskDetails{}, Unavailable()
	}
	store, ok := s.store.(TaskStore)
	if !ok {
		return TaskDetails{}, Unavailable()
	}
	result, err := store.LoadTaskDetails(ctx, id)
	if err != nil {
		var f *failure.Failure
		if errors.As(err, &f) {
			return TaskDetails{}, failure.Safe(f)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return TaskDetails{}, Unavailable()
		}
		return TaskDetails{}, Internal()
	}
	if result.WorkspaceRevision < 1 || result.WorkspaceRevision > MaxRevision || result.Task.ID != id {
		return TaskDetails{}, Internal()
	}
	return result, nil
}
