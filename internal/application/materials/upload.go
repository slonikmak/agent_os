// Package materials owns explicit local text uploads and their durable association.
package materials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
)

const MaxCharacters = 262144
const MaxBytes = 4 * MaxCharacters
const Operation = "materials.upload"

type Upload struct {
	ExpectedTaskRevision int64
	MediaType, Text      string
	Label                *string
	AppliedBy            access.Principal
}
type Result struct {
	WorkspaceRevision int64
	TaskRef           queries.TaskRef
	MaterialID        string
}
type Receipt struct {
	RequestHash string
	Status      int
	Result      Result
}
type Record struct {
	ID, TaskID, Digest, MediaType, UploadedAt string
	ByteCount                                 int64
	Label                                     *string
	AppliedBy                                 access.Principal
	TaskRevision                              int64
}
type TaskState struct {
	Revision     int64
	Associations int
}
type Tx interface {
	FindMaterialReceipt(context.Context, string, string) (Receipt, bool, error)
	MaterialTask(context.Context, string) (TaskState, error)
	Revision(context.Context, bool) (int64, error)
	InsertMaterial(context.Context, Record) error
	InsertMaterialReceipt(context.Context, string, string, Receipt) error
}
type Work interface {
	WithinMaterials(context.Context, func(Tx) error) error
}

// Content publishes durable bytes without granting query authority. The registry
// and receipt are committed only after this succeeds; abandoned blobs stay inert.
type Content interface {
	Put(context.Context, string, []byte) error
}
type Uploader interface {
	UploadMaterial(context.Context, string, string, Upload) (Result, int, error)
}
type Service struct {
	work    Work
	content Content
	changed func(int64)
}

func New(work Work, content Content, changed func(int64)) *Service {
	return &Service{work, content, changed}
}
func ValidText(text string) bool {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxCharacters {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\t' && r != '\r' && r != '\n' {
			return false
		}
	}
	return true
}
func (s *Service) UploadMaterial(ctx context.Context, id, key string, in Upload) (result Result, status int, err error) {
	defer func() {
		if recover() != nil {
			result = Result{}
			status = 0
			err = failure.RecoveredPanic()
		}
	}()
	if !access.Allowed(in.AppliedBy, access.UploadMaterial) {
		return result, 0, failure.New("forbidden")
	}
	if !queries.ValidID(id) || len(key) < 1 || len(key) > 128 || !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", rune(key[0])) || strings.Trim(key, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._:") != "" || in.ExpectedTaskRevision < 1 || in.ExpectedTaskRevision > queries.MaxRevision || (in.MediaType != "text/plain" && in.MediaType != "text/markdown") || !ValidText(in.Text) || (in.Label != nil && (!utf8.ValidString(*in.Label) || strings.TrimSpace(*in.Label) == "" || utf8.RuneCountInString(*in.Label) > 256)) {
		return result, 0, failure.New("invalid_request")
	}
	data, _ := json.Marshal(struct {
		Version           int
		Operation, Target string
		Input             struct {
			Revision        int64
			MediaType, Text string
			Label           *string
		}
	}{1, Operation, id, struct {
		Revision        int64
		MediaType, Text string
		Label           *string
	}{in.ExpectedTaskRevision, in.MediaType, in.Text, in.Label}})
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	bytes := []byte(in.Text)
	digest := sha256.Sum256(bytes)
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	fresh := false
	err = s.work.WithinMaterials(ctx, func(tx Tx) error {
		receipt, found, e := tx.FindMaterialReceipt(ctx, in.AppliedBy.ClientID, key)
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
		task, e := tx.MaterialTask(ctx, id)
		if e != nil {
			return e
		}
		if task.Revision != in.ExpectedTaskRevision {
			f := failure.New("revision_conflict")
			f.CurrentRevision = &task.Revision
			return f
		}
		if task.Associations >= 32 {
			return failure.New("unsupported_operation")
		}
		if task.Revision == queries.MaxRevision {
			return failure.New("service_unavailable")
		}
		revision, e := tx.Revision(ctx, false)
		if e != nil {
			return e
		}
		if revision >= queries.MaxRevision {
			return failure.New("service_unavailable")
		}
		var random [16]byte
		if _, e = rand.Read(random[:]); e != nil {
			return e
		}
		record := Record{ID: "M-" + hex.EncodeToString(random[:]), TaskID: id, Digest: hex.EncodeToString(digest[:]), ByteCount: int64(len(bytes)), MediaType: in.MediaType, Label: in.Label, AppliedBy: in.AppliedBy, UploadedAt: time.Now().UTC().Format(time.RFC3339Nano), TaskRevision: task.Revision + 1}
		if e = s.content.Put(ctx, record.Digest, bytes); e != nil {
			return e
		}
		if e = tx.InsertMaterial(ctx, record); e != nil {
			return e
		}
		revision, e = tx.Revision(ctx, true)
		if e != nil {
			return e
		}
		result, status = Result{revision, queries.TaskRef{TaskID: id, Revision: record.TaskRevision}, record.ID}, 201
		if e = tx.InsertMaterialReceipt(ctx, in.AppliedBy.ClientID, key, Receipt{hash, status, result}); e != nil {
			return e
		}
		fresh = true
		return nil
	})
	if err != nil {
		var f *failure.Failure
		if errors.As(err, &f) {
			return Result{}, 0, failure.Safe(f)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, 0, failure.New("service_unavailable")
		}
		return Result{}, 0, failure.New("internal_error")
	}
	if fresh && s.changed != nil {
		func() { defer func() { _ = recover() }(); s.changed(result.WorkspaceRevision) }()
	}
	return result, status, nil
}
