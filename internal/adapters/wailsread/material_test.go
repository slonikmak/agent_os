package wailsread

import (
	"context"
	"encoding/json"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
	"personaldesk/internal/testsupport/contractfixture"
	"strings"
	"testing"
)

type materialReaderStub struct {
	read func(context.Context, string) (queries.MaterialResult, error)
}

func (materialReaderStub) GetWorkspaceSnapshot(context.Context) (queries.WorkspaceSnapshot, error) {
	return contractfixture.Snapshot(), nil
}
func (r materialReaderStub) ReadMaterial(ctx context.Context, id string) (queries.MaterialResult, error) {
	return r.read(ctx, id)
}
func TestMaterialNativeBudgetsFaultsAndExpectedFailures(t *testing.T) {
	for _, name := range []string{"saved", "panic", "expected", "schema", "large"} {
		t.Run(name, func(t *testing.T) {
			detail := queries.MaterialResult{WorkspaceRevision: 3, Material: queries.Material{ID: "M-one", Availability: "available", MediaType: ptr("text/plain"), Text: ptr("Saved Unicode text \u2603")}}
			reader := materialReaderStub{func(ctx context.Context, id string) (queries.MaterialResult, error) {
				switch name {
				case "saved":
					return detail, nil
				case "panic":
					panic("PRIVATE SQL /private")
				case "expected":
					return queries.MaterialResult{}, failure.New("not_found")
				case "schema":
					return queries.MaterialResult{}, nil
				default:
					text := strings.Repeat("x", 4<<20)
					media := "text/plain"
					return queries.MaterialResult{WorkspaceRevision: 1, Material: queries.Material{ID: id, Availability: "available", MediaType: &media, Text: &text}}, nil
				}
			}}
			diag := diagnostics.New()
			result := New(reader, diag, "").ReadMaterial(context.Background(), "M-one")
			if name == "saved" {
				want, _ := json.Marshal(v1.FromMaterialResult(detail))
				got, _ := json.Marshal(result.Data)
				if result.Error != nil || string(want) != string(got) {
					t.Fatal("native material mapping drift", result.Error)
				}
				return
			}
			if result.Data != nil || result.Error == nil || strings.Contains(result.Error.Message, "PRIVATE") {
				t.Fatal(result)
			}
			want := "internal_error"
			if name == "expected" {
				want = "not_found"
			}
			if name == "large" {
				want = "response_too_large"
			}
			if result.Error.Code != want {
				t.Fatal(result.Error)
			}
			class := ""
			if want == "internal_error" {
				class = "internal"
				if name == "panic" {
					class = "panic"
				}
			}
			assertNativeFailure(t, diag, result.Error, "material.read", want, class)
		})
	}
}
