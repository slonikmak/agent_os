package queries

import (
	"context"
	"errors"
	"personaldesk/internal/application/failure"
	"strings"
)

type InboxStore interface {
	LoadInboxMessage(context.Context, string) (InboxDetails, error)
}
type InboxReader interface {
	GetInboxMessage(context.Context, string) (InboxDetails, error)
}

func ValidID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", rune(id[0])) && strings.Trim(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._:-") == ""
}
func (s *Service) GetInboxMessage(ctx context.Context, id string) (InboxDetails, error) {
	if !ValidID(id) {
		return InboxDetails{}, failure.New("invalid_request")
	}
	store, ok := s.store.(InboxStore)
	if !ok {
		return InboxDetails{}, Unavailable()
	}
	result, err := store.LoadInboxMessage(ctx, id)
	if err != nil {
		var f *failure.Failure
		if errors.As(err, &f) {
			return InboxDetails{}, failure.Safe(f)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return InboxDetails{}, Unavailable()
		}
		return InboxDetails{}, Internal()
	}
	if result.WorkspaceRevision < 1 || result.WorkspaceRevision > MaxRevision || result.Message.ID != id {
		return InboxDetails{}, Internal()
	}
	return result, nil
}
