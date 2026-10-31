package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
)

const ReceiveOperation = "inbox.receive"

// A receipt is the original result, not a pointer to a mutable Inbox record.
type Receipt struct {
	RequestHash string
	Status      int
	Result      ReceiveResult
}
type SavedMessage struct {
	IdentityHash string
	Message      queries.InboxMessage
}

// ReceiveTx is owned by its consuming workflow. Every method uses the same
// transaction; only the UnitOfWork may commit or roll it back.
type ReceiveTx interface {
	FindReceipt(context.Context, string, string, string) (Receipt, bool, error)
	FindMessage(context.Context, string, string, string) (SavedMessage, bool, error)
	AllocateMessage(context.Context) (string, int64, error)
	InsertMessage(context.Context, queries.InboxMessage, string, int64) error
	Revision(context.Context, bool) (int64, error)
	InsertReceipt(context.Context, string, string, string, Receipt) error
}
type UnitOfWork interface {
	Within(context.Context, func(ReceiveTx) error) error
}
type Receiver interface {
	ReceiveMessage(context.Context, string, Receive) (ReceiveResult, int, error)
}
type ReceiveService struct {
	work    UnitOfWork
	now     func() time.Time
	changed func(int64)
}

func NewReceiver(work UnitOfWork, changed func(int64)) *ReceiveService {
	return &ReceiveService{work: work, now: time.Now, changed: changed}
}

// Hash only the versioned operation, target, and typed normalized client input.
// Do not trim text, rewrite timestamps or sort artifacts: those are original facts.
func receiveHash(input Receive) (string, error) {
	if input.Artifacts == nil {
		input.Artifacts = []queries.ArtifactCoordinate{}
	}
	data, err := json.Marshal(struct {
		Version           int
		Operation, Target string
		Input             struct {
			Source, Scope, MessageID, Kind, Text string
			Sender, OccurredAt, SourceURL        *string
			TaskRef                              *queries.TaskRef
			Artifacts                            []queries.ArtifactCoordinate
		}
	}{1, ReceiveOperation, "/api/v1/inbox/messages", struct {
		Source, Scope, MessageID, Kind, Text string
		Sender, OccurredAt, SourceURL        *string
		TaskRef                              *queries.TaskRef
		Artifacts                            []queries.ArtifactCoordinate
	}{input.Source, input.Scope, input.MessageID, input.Kind, input.Text, input.Sender, input.OccurredAt, input.SourceURL, input.TaskRef, input.Artifacts}})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func validKey(key string) bool {
	return len(key) > 0 && len(key) <= 128 && strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", rune(key[0])) && strings.Trim(key, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._:-") == ""
}
func (s *ReceiveService) ReceiveMessage(ctx context.Context, key string, input Receive) (result ReceiveResult, status int, err error) {
	defer func() {
		if recover() != nil {
			result = ReceiveResult{}
			status = 0
			err = failure.RecoveredPanic()
		}
	}()
	if !access.Allowed(input.SubmittedBy, access.Receive) {
		return result, 0, failure.New("forbidden")
	}
	if !validKey(key) {
		return result, 0, failure.New("invalid_request")
	}
	hash, e := receiveHash(input)
	if e != nil {
		return result, 0, failure.New("internal_error")
	}
	// Gate waiting and SQL work share one finite deadline, also cancelled by the
	// caller/workspace lifetime. There is no unbounded queue of pending writers.
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	fresh := false
	err = s.work.Within(ctx, func(tx ReceiveTx) error {
		receipt, found, e := tx.FindReceipt(ctx, input.SubmittedBy.ClientID, ReceiveOperation, key)
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
		saved, found, e := tx.FindMessage(ctx, input.Source, input.Scope, input.MessageID)
		if e != nil {
			return e
		}
		if found {
			if saved.IdentityHash == "" || saved.IdentityHash != hash {
				return failure.New("message_identity_conflict")
			}
			revision, e := tx.Revision(ctx, false)
			if e != nil {
				return e
			}
			result, status = ReceiveResult{revision, saved.Message}, 200
		} else {
			id, sequence, e := tx.AllocateMessage(ctx)
			if e != nil {
				return e
			}
			message := queries.InboxMessage{InboxMetadata: queries.InboxMetadata{ID: id, Source: input.Source, Scope: input.Scope, Kind: input.Kind, Status: "pending", Sender: input.Sender, OccurredAt: input.OccurredAt, ReceivedAt: s.now().UTC().Format(time.RFC3339Nano), Revision: 1}, MessageID: input.MessageID, Text: input.Text, SourceURL: input.SourceURL, SubmittedBy: input.SubmittedBy, ProposedTaskRef: input.TaskRef, Artifacts: []queries.MaterialAssociation{}}
			for _, coordinate := range input.Artifacts {
				reason := "not_imported"
				message.Artifacts = append(message.Artifacts, queries.MaterialAssociation{Coordinate: coordinate, Availability: "unavailable", UnavailableReason: &reason})
			}
			if e = tx.InsertMessage(ctx, message, hash, sequence); e != nil {
				return e
			}
			revision, e := tx.Revision(ctx, true)
			if e != nil {
				return e
			}
			result, status = ReceiveResult{revision, message}, 201
			fresh = true
		}
		return tx.InsertReceipt(ctx, input.SubmittedBy.ClientID, ReceiveOperation, key, Receipt{hash, status, result})
	})
	if err != nil {
		var f *failure.Failure
		if errors.As(err, &f) {
			return ReceiveResult{}, 0, failure.Safe(f)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ReceiveResult{}, 0, failure.New("service_unavailable")
		}
		return ReceiveResult{}, 0, failure.New("internal_error")
	}
	if fresh {
		s.notify(result.WorkspaceRevision)
	}
	return result, status, nil
}

// The commit is already durable. Publication failure must never change success
// or cause callers to repeat a command whose first result was committed.
func (s *ReceiveService) notify(revision int64) {
	defer func() { _ = recover() }()
	if s.changed != nil {
		s.changed(revision)
	}
}
