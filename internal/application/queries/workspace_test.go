package queries

import (
	"context"
	"errors"
	"testing"
)

type storeFunc func(context.Context) (WorkspaceSnapshot, error)

func (f storeFunc) LoadWorkspace(ctx context.Context) (WorkspaceSnapshot, error) { return f(ctx) }
func TestServicePreservesPersistedSnapshot(t *testing.T) {
	saved := WorkspaceSnapshot{WorkspaceID: "persisted", WorkspaceRevision: 42, Tasks: []TaskSummary{}, Inbox: []InboxMetadata{}, Sources: []SourceHealth{}}
	service := New(storeFunc(func(context.Context) (WorkspaceSnapshot, error) { return saved, nil }))
	got, err := service.GetWorkspaceSnapshot(context.Background())
	if err != nil || got.WorkspaceRevision != 42 || got.WorkspaceID != "persisted" {
		t.Fatalf("snapshot=%+v error=%v", got, err)
	}
}
func TestServiceSafeFailuresAndCancellation(t *testing.T) {
	service := New(storeFunc(func(context.Context) (WorkspaceSnapshot, error) {
		return WorkspaceSnapshot{}, errors.New("SECRET SQL /private/path")
	}))
	_, err := service.GetWorkspaceSnapshot(context.Background())
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "internal_error" || failure.Error() != "The workspace could not be read" {
		t.Fatalf("unsafe failure %v", err)
	}
	called := false
	service = New(storeFunc(func(context.Context) (WorkspaceSnapshot, error) { called = true; return WorkspaceSnapshot{}, nil }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.GetWorkspaceSnapshot(ctx)
	if called || !errors.As(err, &failure) || failure.Code != "service_unavailable" {
		t.Fatalf("cancelled query called store: %v, %v", called, err)
	}
	for _, revision := range []int64{0, -1, MaxRevision + 1} {
		service = New(storeFunc(func(context.Context) (WorkspaceSnapshot, error) {
			return WorkspaceSnapshot{WorkspaceID: "id", WorkspaceRevision: revision}, nil
		}))
		if _, err := service.GetWorkspaceSnapshot(context.Background()); err == nil {
			t.Fatal("accepted invalid revision")
		}
	}
}
