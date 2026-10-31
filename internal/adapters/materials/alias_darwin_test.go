package materials

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestFinderAliasIsInert(t *testing.T) {
	c, root := fixture(t)
	data := []byte("alias")
	digest := hash(data)
	path := filepath.Join(root, "materials", digest)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	finder := make([]byte, 32)
	finder[8] = 0x80
	if err := unix.Setxattr(path, "com.apple.FinderInfo", finder, 0); err != nil {
		t.Fatal(err)
	}
	got, reason, err := c.Read(context.Background(), digest)
	if len(got) != 0 || reason != "unsupported" || err != nil {
		t.Fatal("Finder alias was read", reason, err)
	}
}
