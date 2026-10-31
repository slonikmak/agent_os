package bootstrap

import (
	"context"
	"personaldesk/internal/application/queries"
)

func (w *Workspace) GetTaskDetails(ctx context.Context, id string) (queries.TaskDetails, error) {
	if w.lifetime.Err() != nil {
		return queries.TaskDetails{}, queries.Unavailable()
	}
	merged, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.lifetime, cancel)
	defer cancel()
	defer stop()
	return w.reader.(queries.TaskReader).GetTaskDetails(merged, id)
}
