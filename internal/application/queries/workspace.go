// Package queries owns the transport-independent saved-workspace read contract.
package queries

import (
	"context"
	"errors"
	"personaldesk/internal/application/failure"
)

const MaxRevision int64 = 9007199254740991

type TaskSummary struct {
	ID, Kind, Status, Title        string
	Summary, NextAction, NextActor *string
	Revision                       int64
	UpdatedAt                      string
}
type InboxMetadata struct {
	ID, Source, Scope, Kind, Status string
	Sender, OccurredAt              *string
	ReceivedAt                      string
	Revision                        int64
}
type SourceHealth struct {
	Source, Scope, Status        string
	LastAttemptAt, LastSuccessAt *string
}
type Counts struct{ Tasks, Inbox, UnprocessedInbox, NeedsAttention int64 }
type WorkspaceSnapshot struct {
	WorkspaceID       string
	WorkspaceRevision int64
	CreatedAt         string
	Tasks             []TaskSummary
	Inbox             []InboxMetadata
	Sources           []SourceHealth
	Counts            Counts
}

// Store is owned by this query, and reads all fields from one committed snapshot.
type Store interface {
	LoadWorkspace(context.Context) (WorkspaceSnapshot, error)
}
type Reader interface {
	GetWorkspaceSnapshot(context.Context) (WorkspaceSnapshot, error)
}
type Service struct {
	store   Store
	content MaterialContent
}

func New(store Store) *Service { return &Service{store: store} }
func (s *Service) GetWorkspaceSnapshot(ctx context.Context) (WorkspaceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return WorkspaceSnapshot{}, Unavailable()
	}
	snapshot, err := s.store.LoadWorkspace(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return WorkspaceSnapshot{}, Unavailable()
		}
		return WorkspaceSnapshot{}, Internal()
	}
	if snapshot.WorkspaceID == "" || snapshot.WorkspaceRevision < 1 || snapshot.WorkspaceRevision > MaxRevision {
		return WorkspaceSnapshot{}, Internal()
	}
	return snapshot, nil
}

// Failure contains only fixed public outcomes; raw causes remain in adapters.
type Failure = failure.Failure

func Internal() *Failure    { return failure.New("internal_error") }
func Unavailable() *Failure { return failure.New("service_unavailable") }
