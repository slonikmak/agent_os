package bootstrap

import (
	"context"
	"personaldesk/internal/application/queries"
	"testing"
	"time"
)

type waitingTaskReader struct{ entered chan struct{} }

func (r waitingTaskReader) GetWorkspaceSnapshot(context.Context) (queries.WorkspaceSnapshot, error) {
	return queries.WorkspaceSnapshot{}, nil
}
func (r waitingTaskReader) GetTaskDetails(ctx context.Context, _ string) (queries.TaskDetails, error) {
	close(r.entered)
	<-ctx.Done()
	return queries.TaskDetails{}, queries.Unavailable()
}
func TestTaskReadLifetimeCancellation(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := waitingTaskReader{entered: make(chan struct{})}
	w := &Workspace{lifetime: lifetime, reader: reader}
	done := make(chan error, 1)
	go func() { _, err := w.GetTaskDetails(context.Background(), "T-one"); done <- err }()
	<-reader.entered
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled task read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("task read outlived workspace")
	}
}
