//go:build darwin || linux

package materials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	app "personaldesk/internal/application/materials"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Content, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, root
}
func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func TestDurableBoundsAndUnsafeLeaves(t *testing.T) {
	c, root := fixture(t)
	ctx := context.Background()
	for _, data := range [][]byte{{}, []byte("saved 👋"), []byte(strings.Repeat("🙂", app.MaxCharacters))} {
		digest := hash(data)
		if err := c.Put(ctx, digest, data); err != nil {
			t.Fatal(err)
		}
		read, reason, err := c.Read(ctx, digest)
		if err != nil || reason != "" || string(read) != string(data) {
			t.Fatal(reason, err)
		}
		if err = c.Put(ctx, digest, data); err != nil {
			t.Fatal("dedup", err)
		}
	}
	digest := hash([]byte("private"))
	path := filepath.Join(root, "materials", digest)
	target := filepath.Join(root, "credentials.json")
	if err := os.WriteFile(target, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink", "hardlink", "directory", "fifo", "corrupt", "oversized"} {
		switch kind {
		case "symlink":
			_ = os.Symlink(target, path)
		case "hardlink":
			_ = os.Link(target, path)
		case "directory":
			_ = os.Mkdir(path, 0700)
		case "fifo":
			_ = unix.Mkfifo(path, 0600)
		case "corrupt":
			_ = os.WriteFile(path, []byte("excluded bytes"), 0600)
		case "oversized":
			_ = os.WriteFile(path, []byte(strings.Repeat("x", app.MaxBytes+1)), 0600)
		}
		data, reason, err := c.Read(ctx, digest)
		if len(data) != 0 || reason == "" || err != nil {
			t.Fatalf("unsafe %s: %d %s %v", kind, len(data), reason, err)
		}
		if err = c.Put(ctx, digest, []byte("private")); err == nil {
			t.Fatal("reused unsafe blob", kind)
		}
		_ = os.Remove(path)
	}
	for _, bad := range []string{"../credentials.json", strings.Repeat("a", 63), "/etc/passwd"} {
		if data, reason, _ := c.Read(ctx, bad); len(data) != 0 || reason != "unsupported" {
			t.Fatal("digest traversal")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := c.Read(ctx, hash(nil)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestAnchoredAncestorsAndReplacement(t *testing.T) {
	c, root := fixture(t)
	data := []byte("saved")
	digest := hash(data)
	if err := c.Put(context.Background(), digest, data); err != nil {
		t.Fatal(err)
	}
	aliasPath := root + "-alias"
	if err := os.Symlink(root, aliasPath); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(aliasPath)
	if opened, err := Open(aliasPath); err == nil {
		opened.Close()
		t.Fatal("ancestor symlink followed")
	}
	old := filepath.Join(root, "materials")
	retained := filepath.Join(root, "original-materials")
	if err := os.Rename(old, retained); err != nil {
		t.Fatal(err)
	}
	private := t.TempDir()
	if err := os.Symlink(private, old); err != nil {
		t.Fatal(err)
	}
	if got, reason, err := c.Read(context.Background(), digest); err != nil || reason != "" || string(got) != "saved" {
		t.Fatal("path replacement redirected anchor", reason, err)
	}
	if opened, err := Open(root); err == nil {
		opened.Close()
		t.Fatal("leaf symlink followed")
	}
}
func TestPublicationFaultsNeverInventReadableContent(t *testing.T) {
	for _, stage := range []string{"write", "sync", "publish", "directory_sync"} {
		t.Run(stage, func(t *testing.T) {
			c, _ := fixture(t)
			data := []byte(stage)
			c.fault = func(at string) error {
				if at == stage {
					return errors.New("fault")
				}
				return nil
			}
			if err := c.Put(context.Background(), hash(data), data); err == nil {
				t.Fatal("fault ignored")
			}
			c.fault = nil
			if err := c.Put(context.Background(), hash(data), data); err != nil {
				t.Fatal("safe retry failed", err)
			}
		})
	}
}
