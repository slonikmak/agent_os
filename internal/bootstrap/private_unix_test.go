//go:build darwin || linux

package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"personaldesk/internal/adapters/diagnostics"
)

func TestFIFOStartupRefusalIsBoundedAndReleasesLock(t *testing.T) {
	for _, name := range []string{"workspace.sqlite", "workspace.sqlite-wal", "workspace.sqlite-shm", "credentials.json", "discovery.json"} {
		t.Run(name, func(t *testing.T) {
			root := newRoot(t)
			if err := privateRoot(root); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mkfifo(filepath.Join(root, name), 0600); err != nil {
				t.Fatal(err)
			}
			// A regression must time out a disposable process, never hang the test runner.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFIFOStartupHelper$")
			command.Env = append(os.Environ(), "PERSONAL_DESK_FIFO_HELPER="+root)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("FIFO startup did not return within its bound: %v", ctx.Err())
			}
			if err != nil {
				t.Fatalf("FIFO helper: %s; %v", output, err)
			}
			lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
			if err != nil {
				t.Fatalf("FIFO startup retained workspace ownership: %v", err)
			}
			if err = releaseLock(lock); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFIFOStartupHelper(t *testing.T) {
	root := os.Getenv("PERSONAL_DESK_FIFO_HELPER")
	if root == "" {
		return
	}
	workspace, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
	if workspace != nil {
		workspace.Close()
		t.Fatal("FIFO service file was accepted")
	}
	var startup *StartupError
	if !errors.As(err, &startup) || startup.Code != "workspace_unsafe" {
		t.Fatalf("FIFO startup result: %v", err)
	}
	// Reacquire before exiting: OS cleanup on child exit alone cannot prove that
	// the failed startup's own cleanup released its lock.
	lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
	if err != nil {
		t.Fatalf("failed startup did not release its lock: %v", err)
	}
	if err = releaseLock(lock); err != nil {
		t.Fatal(err)
	}
}
