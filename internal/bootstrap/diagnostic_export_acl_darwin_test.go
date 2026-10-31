//go:build darwin && cgo

package bootstrap

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticExportStripsInheritedMacAllowACLsBeforeWriting(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// This native everyone allow entry bypasses 0700/0600 mode privacy on macOS.
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,search,file_inherit,directory_inherit", parent).CombinedOutput(); err != nil {
		t.Fatalf("install inheritable ACL: %v: %s", err, output)
	}
	acl := func(path string) string {
		t.Helper()
		out, err := exec.Command("/bin/ls", "-lde", path).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	controlDir := filepath.Join(parent, "control")
	if err = os.Mkdir(controlDir, 0700); err != nil {
		t.Fatal(err)
	}
	controlFile := filepath.Join(controlDir, "file")
	if err = os.WriteFile(controlFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{controlDir, controlFile} {
		if output := acl(path); !strings.Contains(output, "everyone inherited allow") {
			t.Fatalf("fixture did not inherit native allow ACL: %s", output)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = privatizeDiagnosticExport(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if output := acl(path); len(strings.Split(strings.TrimSpace(output), "\n")) != 1 {
			t.Fatalf("descriptor retained ACL: %s", output)
		}
	}
	target := filepath.Join(parent, "private.zip")
	wrote := false
	err = publishDiagnosticArchive(context.Background(), target, nil, func(writer io.Writer) error {
		entries, err := os.ReadDir(parent)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".personaldesk-export-") {
				stage := filepath.Join(parent, entry.Name())
				for _, path := range []string{stage, filepath.Join(stage, "archive")} {
					if output := acl(path); len(strings.Split(strings.TrimSpace(output), "\n")) != 1 {
						t.Errorf("ACL visible before diagnostic bytes: %s", output)
					}
				}
			}
		}
		wrote = true
		_, err = io.WriteString(writer, "private diagnostic bytes")
		return err
	})
	if err != nil || !wrote {
		t.Fatalf("private export: %v, wrote=%v", err, wrote)
	}
	if output := acl(target); len(strings.Split(strings.TrimSpace(output), "\n")) != 1 {
		t.Fatalf("published inode retained ACL: %s", output)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("published mode")
	}
	if output := acl(parent); !strings.Contains(output, "everyone allow") {
		t.Fatal("selected parent ACL was modified")
	}
}
