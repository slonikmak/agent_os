package sqlite

import (
	"context"
	"errors"
	"personaldesk/internal/application/materials"
	"personaldesk/internal/application/queries"
	"testing"
)

type materialContentFunc func(context.Context, string, []byte) error

func (f materialContentFunc) Put(ctx context.Context, digest string, data []byte) error {
	return f(ctx, digest, data)
}
func TestMaterialCancellationAndWriteFaultsNeverGrantRegistryAuthority(t *testing.T) {
	for _, mode := range []string{"write", "durable-cancel", "precommit-panic", "workspace-ceiling", "task-ceiling"} {
		t.Run(mode, func(t *testing.T) {
			s, content, _, id := materialFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			in := uploadInput()
			bytes := materialContentFunc(func(ctx context.Context, digest string, data []byte) error {
				calls++
				if mode == "write" {
					return errors.New("PRIVATE")
				}
				if err := content.Put(ctx, digest, data); err != nil {
					return err
				}
				if mode == "durable-cancel" {
					cancel()
				}
				return nil
			})
			if mode == "precommit-panic" {
				s.beforeCommit = func() error { panic("PRIVATE") }
			}
			if mode == "workspace-ceiling" {
				_, _ = s.writer.Exec(`UPDATE workspace_meta SET workspace_revision=?`, queries.MaxRevision)
			}
			if mode == "task-ceiling" {
				_, _ = s.writer.Exec(`UPDATE tasks SET revision=?`, queries.MaxRevision)
				in.ExpectedTaskRevision = queries.MaxRevision
			}
			before := creationFacts(t, s)
			_, _, err := materials.New(s, bytes, nil).UploadMaterial(ctx, id, "upload", in)
			if mode == "write" || mode == "precommit-panic" {
				requireCode(t, err, "internal_error")
			} else {
				requireCode(t, err, "service_unavailable")
			}
			if before != creationFacts(t, s) {
				t.Fatal("failure changed committed facts")
			}
			var count int
			_ = s.writer.QueryRow(`SELECT count(*) FROM task_materials`).Scan(&count)
			if count != 0 {
				t.Fatal("failed publication granted authority")
			}
			if (mode == "workspace-ceiling" || mode == "task-ceiling") && calls != 0 {
				t.Fatal("revision refusal wrote bytes")
			}
		})
	}
}
