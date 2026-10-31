//go:build darwin || linux

package bootstrap

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"personaldesk/internal/adapters/diagnostics"
	v1 "personaldesk/internal/contracts/v1"
)

func inspectArchive(t *testing.T, path string) v1.Manifest {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	names := v1.ArchiveEntries()
	if len(z.File) != len(names) {
		t.Fatal("entry count")
	}
	var manifest v1.Manifest
	for i, f := range z.File {
		if f.Name != names[i] || f.Mode().Perm() != 0600 || !f.Mode().IsRegular() {
			t.Fatal("entry policy")
		}
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		if bytes.Contains(data, []byte("SYNTHETIC_SECRET")) || bytes.Contains(data, []byte(filepath.Dir(path))) {
			t.Fatal("privacy leak")
		}
		switch i {
		case 0:
			if _, err = v1.Validate("manifest", bytes.NewReader(data), v1.MaxDiagnosticRecordBytes); err != nil {
				t.Fatal(err)
			}
			json.Unmarshal(data, &manifest)
		case 1:
			for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				if _, err = v1.Validate("diagnostic-event", bytes.NewReader(line), v1.MaxDiagnosticRecordBytes); err != nil {
					t.Fatal(err)
				}
			}
		case 2:
			if len(data) != 0 {
				t.Fatal("template")
			}
		}
	}
	return manifest
}
func TestMaintenanceExportCachedSchemaSurvivesHaltAndUnavailableDB(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "failed"}[failed], func(t *testing.T) {
			root := newRoot(t)
			b := diagnostics.New()
			defer b.Close()
			if failed {
				privateRoot(root)
				os.WriteFile(filepath.Join(root, "workspace.sqlite"), []byte("SYNTHETIC_SECRET invalid database"), 0600)
			}
			w, err := StartSession(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, b)
			if w == nil || failed != (err != nil) {
				t.Fatalf("startup %v", err)
			}
			defer w.Close()
			target := filepath.Join(filepath.Dir(root), "result.zip")
			m := NewDiagnosticMaintenance(b, root, w, func() (string, error) { return target, nil })
			w.Halt()
			result := m.ExportDiagnostics()
			if result.Status != "exported" || result.ErrorID != nil || result.Coverage == nil {
				t.Fatalf("result %+v", result)
			}
			manifest := inspectArchive(t, target)
			if failed {
				if manifest.SchemaStatus != "unsupported" && manifest.SchemaStatus != "unavailable" {
					t.Fatal("failed schema fact")
				}
				if manifest.SchemaVersionApplied != nil {
					t.Fatal("invented failed schema version")
				}
			} else if manifest.SchemaStatus != "ready" || manifest.SchemaVersionApplied == nil {
				t.Fatal("lost cached schema")
			}
			if info, _ := os.Stat(target); info.Mode().Perm() != 0600 {
				t.Fatal("public archive")
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			if got := m.ExportDiagnostics(); got.Status != "cancelled" {
				t.Fatal("late export")
			}
		})
	}
}
func TestMaintenanceCancelBusyLatePickerAndFailureDoNotTouchSharedFiles(t *testing.T) {
	b := diagnostics.New()
	defer b.Close()
	b.Record("app.start", "startup", "", "")
	entered, release := make(chan struct{}), make(chan struct{})
	root := newRoot(t)
	path := filepath.Join(filepath.Dir(root), "late.zip")
	m := NewDiagnosticMaintenance(b, root, nil, func() (string, error) { close(entered); <-release; return path, nil })
	done := make(chan v1.ExportResult, 1)
	go func() { done <- m.ExportDiagnostics() }()
	<-entered
	if got := m.ExportDiagnostics(); got.Status != "failed" || got.ErrorID == nil {
		t.Fatal("busy queued another picker")
	}
	m.Close()
	close(release)
	if got := <-done; got.Status != "cancelled" || got.Coverage != nil || got.ErrorID != nil {
		t.Fatal("late picker result")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late file")
	}
	if len(b.Snapshot()) != 1 {
		t.Fatal("expected cancellation/refusal logged as bug")
	}
	cancel := NewDiagnosticMaintenance(b, root, nil, func() (string, error) { return "", nil })
	defer cancel.Close()
	if got := cancel.ExportDiagnostics(); got.Status != "cancelled" || got.ErrorID != nil || got.Coverage != nil {
		t.Fatal("cancel contract")
	}
	panics := NewDiagnosticMaintenance(b, root, nil, func() (string, error) { panic("SYNTHETIC_SECRET") })
	defer panics.Close()
	if got := panics.ExportDiagnostics(); got.Status != "failed" || got.ErrorID == nil {
		t.Fatal("picker panic escaped")
	}
	export := NewDiagnosticMaintenance(b, root, nil, func() (string, error) { return path, nil })
	defer export.Close()
	if got := export.ExportDiagnostics(); got.Status != "exported" || got.Coverage.Complete {
		t.Fatal("private export")
	}
	manifest := inspectArchive(t, path)
	if manifest.SharedDiagnostics != "unavailable" || manifest.SchemaStatus != "unknown" {
		t.Fatal("private availability")
	}
}
func TestSafeDiagnosticDestinationAliasesProtectedIdentityAndIOFailures(t *testing.T) {
	parent, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(parent, "protected")
	os.Mkdir(root, 0700)
	info, _ := os.Stat(root)
	protection := []protectedDestination{{root, info}}
	write := func(w io.Writer) error { _, err := io.WriteString(w, "synthetic archive"); return err }
	assertRefusal := func(path string) {
		t.Helper()
		if err := publishDiagnosticArchive(context.Background(), path, protection, write); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	assertRefusal(filepath.Join(root, "report.zip"))
	assertRefusal(filepath.Join(strings.ToUpper(root), "report.zip"))
	existing := filepath.Join(parent, "existing.zip")
	os.WriteFile(existing, []byte("KEEP"), 0600)
	assertRefusal(existing)
	link := filepath.Join(parent, "link.zip")
	os.Symlink(existing, link)
	assertRefusal(link)
	hard := filepath.Join(parent, "hard.zip")
	os.Link(existing, hard)
	assertRefusal(hard)
	alias := filepath.Join(parent, "alias")
	os.Symlink(parent, alias)
	assertRefusal(filepath.Join(alias, "report.zip"))
	moved := filepath.Join(parent, "moved")
	os.Rename(root, moved)
	assertRefusal(filepath.Join(moved, "report.zip"))
	if data, _ := os.ReadFile(existing); string(data) != "KEEP" {
		t.Fatal("overwrote original")
	}
	for _, mode := range []string{"write", "sync", "publish", "panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			target := filepath.Join(parent, mode+".zip")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			callback := func(w io.Writer) error {
				io.WriteString(w, "partial")
				switch mode {
				case "write":
					return io.ErrShortWrite
				case "sync":
					w.(*os.File).Close()
				case "publish":
					os.WriteFile(target, []byte("KEEP"), 0600)
				case "panic":
					panic("SYNTHETIC_SECRET")
				case "cancel":
					cancel()
				}
				return nil
			}
			var err error
			func() {
				defer func() {
					if mode == "panic" && recover() == nil {
						t.Error("missing injected panic")
					}
				}()
				err = publishDiagnosticArchive(ctx, target, nil, callback)
			}()
			if mode != "panic" && err == nil {
				t.Fatal("fault not returned")
			}
			data, e := os.ReadFile(target)
			if mode == "publish" {
				if string(data) != "KEEP" {
					t.Fatal("publish overwrote racing file")
				}
			} else if !errors.Is(e, os.ErrNotExist) {
				t.Fatal("partial published")
			}
			files, _ := os.ReadDir(parent)
			for _, file := range files {
				if strings.HasPrefix(file.Name(), ".personaldesk-export-") {
					t.Fatal("staging leak")
				}
			}
		})
	}
}
func TestCaptureRotationExportAndCloseDrainBeforeUnlock(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	b := w.diag
	path := filepath.Join(filepath.Dir(root), "race.zip")
	m := NewDiagnosticMaintenance(b, root, w, func() (string, error) { return path, nil })
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 150 {
				b.Record("api.operation", "workspace.read", diagnostics.ID(), "")
			}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); m.ExportDiagnostics() }()
	wg.Add(1)
	go func() { defer wg.Done(); w.Close() }()
	wg.Wait()
	markerBefore, _ := os.ReadFile(filepath.Join(root, "logs", "session.json"))
	b.Record("app.start", "startup", "", "")
	b.Close()
	markerAfter, _ := os.ReadFile(filepath.Join(root, "logs", "session.json"))
	if !bytes.Equal(markerBefore, markerAfter) {
		t.Fatal("late shared operation")
	}
	next, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
	if err != nil {
		t.Fatal("ownership not released", err)
	}
	next.Close()
	if _, err = os.Stat(path); err == nil {
		inspectArchive(t, path)
	}
}

func TestDiagnosticPublicationRejectsStagingAndParentReplacement(t *testing.T) {
	for _, mode := range []string{"staging-directory", "staging-file", "parent"} {
		t.Run(mode, func(t *testing.T) {
			base, _ := filepath.EvalSymlinks(t.TempDir())
			parent := filepath.Join(base, "destination")
			os.Mkdir(parent, 0700)
			target := filepath.Join(parent, "report.zip")
			var replacement string
			err := publishDiagnosticArchive(context.Background(), target, nil, func(w io.Writer) error {
				io.WriteString(w, "original complete bytes")
				if mode == "parent" {
					os.Rename(parent, parent+"-moved")
					os.Mkdir(parent, 0700)
					return nil
				}
				entries, _ := os.ReadDir(parent)
				if len(entries) != 1 {
					t.Fatal("staging inventory")
				}
				stage := filepath.Join(parent, entries[0].Name())
				if mode == "staging-directory" {
					os.Rename(stage, stage+"-moved")
					os.Mkdir(stage, 0700)
					replacement = filepath.Join(stage, "KEEP")
					os.WriteFile(replacement, []byte("KEEP"), 0600)
				} else {
					replacement = filepath.Join(stage, "archive")
					os.Remove(replacement)
					os.WriteFile(replacement, []byte("KEEP"), 0600)
				}
				return nil
			})
			if err == nil {
				t.Fatal("replacement accepted")
			}
			if replacement != "" {
				if data, _ := os.ReadFile(replacement); string(data) != "KEEP" {
					t.Fatal("deleted independent replacement")
				}
			}
			if _, err = os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("replacement published")
			}
		})
	}
}

func TestPublishedDiagnosticSyncFailureAndPanicCleanOnlyOwnArchive(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panics], func(t *testing.T) {
			parent, _ := filepath.EvalSymlinks(t.TempDir())
			target := filepath.Join(parent, "result.zip")
			calls := 0
			var err error
			func() {
				defer func() {
					if fault := recover(); panics != (fault != nil) {
						t.Error("panic boundary")
					}
				}()
				err = publishDiagnosticArchiveWithSync(context.Background(), target, nil, func(w io.Writer) error { _, err := io.WriteString(w, "complete archive"); return err }, func(file *os.File) error {
					calls++
					if calls == 2 {
						if panics {
							panic("SYNTHETIC_SECRET")
						}
						return io.ErrClosedPipe
					}
					return file.Sync()
				})
			}()
			if !panics && err == nil {
				t.Fatal("sync error lost")
			}
			if _, err = os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed publish survived")
			}
			files, _ := os.ReadDir(parent)
			if len(files) != 0 {
				t.Fatal("staging leak")
			}
		})
	}
}

func TestExportPinnedInventoryAndUnreadableSharedFallback(t *testing.T) {
	root := newRoot(t)
	privateRoot(root)
	sink, _, err := openDiagnostics(root, diagnostics.ID())
	if err != nil {
		t.Fatal(err)
	}
	b := diagnostics.New()
	defer b.Close()
	b.Attach(sink, false)
	b.Record("app.start", "startup", "", "")
	if _, err = b.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The old directory remains the sole source even after its pathname is replaced.
	logs := filepath.Join(root, "logs")
	pinned := filepath.Join(root, "retained-logs")
	os.Rename(logs, pinned)
	os.Mkdir(logs, 0700)
	os.WriteFile(filepath.Join(logs, logName(0)), []byte("SYNTHETIC_SECRET\n"), 0600)
	snap, err := b.Collect(context.Background())
	if err != nil || snap.Coverage.AcceptedRecords != 1 || bytes.Contains(snap.Logs, []byte("SYNTHETIC_SECRET")) {
		t.Fatalf("snapshot redirected %+v %v", snap.Coverage, err)
	}
	active := filepath.Join(pinned, logName(0))
	os.Chmod(active, 0400)
	b.Record("app.stop", "shutdown", "", "")
	snap, err = b.Collect(context.Background())
	if err != nil || snap.Shared != "unavailable" || snap.Coverage.Complete || snap.Coverage.UnavailableFiles != 1 || snap.Coverage.AcceptedRecords != 2 {
		t.Fatalf("unreadable private fallback %+v %v", snap, err)
	}
	os.Chmod(active, 0600)
}

func TestDiagnosticEarlyStageOpenFailureCleansOnlyCreatedEmptyDirectory(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "EMFILE", true: "independent-replacement"}[replace], func(t *testing.T) {
			parent, _ := filepath.EvalSymlinks(t.TempDir())
			target := filepath.Join(parent, "report.zip")
			wrote := false
			var replacement string
			err := publishDiagnosticArchiveWithIO(context.Background(), target, nil, func(io.Writer) error { wrote = true; return nil }, (*os.File).Sync, func(dirfd int, name string, _ int, _ uint32) (int, error) {
				if replace {
					if err := unix.Renameat(dirfd, name, dirfd, name+"-original"); err != nil {
						t.Fatal(err)
					}
					if err := unix.Mkdirat(dirfd, name, 0700); err != nil {
						t.Fatal(err)
					}
					replacement = filepath.Join(parent, name)
				}
				return -1, unix.EMFILE
			})
			if !errors.Is(err, unix.EMFILE) || wrote {
				t.Fatalf("early failure %v, wrote=%v", err, wrote)
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			if replace {
				if info, err := os.Stat(replacement); err != nil || !info.IsDir() {
					t.Fatal("deleted independent empty replacement")
				}
				if len(entries) != 2 {
					t.Fatal("unexpected replacement cleanup")
				}
			} else if len(entries) != 0 {
				t.Fatal("new empty staging directory leaked")
			}
			if _, err = os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("published after open failure")
			}
		})
	}
}
