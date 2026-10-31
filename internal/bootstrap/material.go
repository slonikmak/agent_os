package bootstrap

import (
	"context"
	"personaldesk/internal/application/queries"
)

func (w *Workspace) ReadMaterial(ctx context.Context, id string) (queries.MaterialResult, error) {
	if w.lifetime.Err() != nil {
		return queries.MaterialResult{}, queries.Unavailable()
	}
	merged, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.lifetime, cancel)
	defer cancel()
	defer stop()
	return w.reader.(queries.MaterialReader).ReadMaterial(merged, id)
}
