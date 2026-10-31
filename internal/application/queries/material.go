package queries

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"unicode"
	"unicode/utf8"

	"personaldesk/internal/application/failure"
)

const MaxMaterialCharacters = 262144

type MaterialRecord struct {
	ID, Digest, MediaType string
	ByteCount             int64
}
type MaterialStore interface {
	LoadMaterial(context.Context, string) (MaterialRecord, int64, error)
}
type MaterialContent interface {
	Read(context.Context, string) ([]byte, string, error)
}
type MaterialReader interface {
	ReadMaterial(context.Context, string) (MaterialResult, error)
}

func NewWithMaterials(store Store, content MaterialContent) *Service {
	return &Service{store: store, content: content}
}
func (s *Service) ReadMaterial(ctx context.Context, id string) (MaterialResult, error) {
	if !ValidID(id) {
		return MaterialResult{}, failure.New("invalid_request")
	}
	if ctx.Err() != nil {
		return MaterialResult{}, Unavailable()
	}
	store, ok := s.store.(MaterialStore)
	if !ok || s.content == nil {
		return MaterialResult{}, Unavailable()
	}
	record, revision, err := store.LoadMaterial(ctx, id)
	if err != nil {
		var f *failure.Failure
		if errors.As(err, &f) {
			return MaterialResult{}, failure.Safe(f)
		}
		if ctx.Err() != nil {
			return MaterialResult{}, Unavailable()
		}
		return MaterialResult{}, Internal()
	}
	if revision < 1 || revision > MaxRevision || record.ID != id {
		return MaterialResult{}, Internal()
	}
	result := MaterialResult{WorkspaceRevision: revision, Material: Material{ID: id, Availability: "unavailable"}}
	unavailable := func(reason string) (MaterialResult, error) {
		result.Material.UnavailableReason = &reason
		return result, nil
	}
	if record.MediaType != "text/plain" && record.MediaType != "text/markdown" {
		return unavailable("unsupported")
	}
	if record.ByteCount > 4*MaxMaterialCharacters {
		return unavailable("too_large")
	}
	data, reason, err := s.content.Read(ctx, record.Digest)
	if err != nil || ctx.Err() != nil {
		return MaterialResult{}, Unavailable()
	}
	if reason != "" {
		if reason != "missing" && reason != "too_large" {
			reason = "unsupported"
		}
		return unavailable(reason)
	}
	if len(data) > 4*MaxMaterialCharacters || utf8.RuneCount(data) > MaxMaterialCharacters {
		return unavailable("too_large")
	}
	if !utf8.Valid(data) || int64(len(data)) != record.ByteCount {
		return unavailable("unsupported")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != record.Digest {
		return unavailable("unsupported")
	}
	text := string(data)
	for _, r := range text {
		if unicode.IsControl(r) && r != '\t' && r != '\r' && r != '\n' {
			return unavailable("unsupported")
		}
	}
	result.Material.Availability = "available"
	result.Material.MediaType = &record.MediaType
	result.Material.Text = &text
	return result, nil
}
