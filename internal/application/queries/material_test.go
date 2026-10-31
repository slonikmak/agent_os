package queries

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

type materialStore struct{ record MaterialRecord }

func (m materialStore) LoadWorkspace(context.Context) (WorkspaceSnapshot, error) {
	return WorkspaceSnapshot{}, nil
}
func (m materialStore) LoadMaterial(context.Context, string) (MaterialRecord, int64, error) {
	return m.record, 7, nil
}

type contentBytes struct {
	data   []byte
	reason string
}

func (c contentBytes) Read(context.Context, string) ([]byte, string, error) {
	return c.data, c.reason, nil
}
func TestMaterialContentValidationUsesCommittedRegistryRevision(t *testing.T) {
	for _, test := range []struct {
		name, text, reason, want string
		count                    int64
		media                    string
	}{
		{name: "empty", text: "", count: 0}, {name: "unicode-limit", text: strings.Repeat("🙂", MaxMaterialCharacters), count: 4 * MaxMaterialCharacters}, {name: "over-limit", text: strings.Repeat("a", MaxMaterialCharacters+1), count: MaxMaterialCharacters + 1, want: "too_large"}, {name: "binary", text: "a\x00", count: 2, want: "unsupported"}, {name: "utf8", text: string([]byte{0xff}), count: 1, want: "unsupported"}, {name: "missing", reason: "missing", want: "missing"}, {name: "length", text: "text", count: 3, want: "unsupported"}, {name: "media", text: "a", count: 1, media: "image/png", want: "unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sum := sha256.Sum256([]byte(test.text))
			media := test.media
			if media == "" {
				media = "text/plain"
			}
			service := NewWithMaterials(materialStore{MaterialRecord{ID: "M-one", Digest: hex.EncodeToString(sum[:]), MediaType: media, ByteCount: test.count}}, contentBytes{[]byte(test.text), test.reason})
			result, err := service.ReadMaterial(context.Background(), "M-one")
			if err != nil || result.WorkspaceRevision != 7 {
				t.Fatal(result, err)
			}
			if test.want == "" {
				if result.Material.Text == nil || *result.Material.Text != test.text || result.Material.UnavailableReason != nil {
					t.Fatal("valid content changed")
				}
			} else if result.Material.Text != nil || result.Material.MediaType != nil || result.Material.UnavailableReason == nil || *result.Material.UnavailableReason != test.want {
				t.Fatal("unavailable content leaked", result.Material)
			}
		})
	}
}
