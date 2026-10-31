package bootstrap

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/sqlite"
	"personaldesk/internal/testsupport/migrationfixture"
	"personaldesk/migrations"
)

func recoveryFixture(t *testing.T) (string, string) {
	t.Helper()
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	f, err := openPrivate(filepath.Join(root, "workspace.sqlite"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = migrationfixture.Populate(filepath.Join(root, "workspace.sqlite")); err != nil {
		t.Fatal(err)
	}

	catalog, _ := migrations.Embedded()
	backupPath := filepath.Join(t.TempDir(), "v1.sqlite")
	if err := os.WriteFile(backupPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.OpenCatalog(context.Background(), filepath.Join(root, "workspace.sqlite"), catalog, func(ctx context.Context, state sqlite.State) error {
		_, err := sqlite.Backup(ctx, filepath.Join(root, "workspace.sqlite"), backupPath, catalog)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err = os.WriteFile(filepath.Join(root, "credentials.json"), []byte("KEEP PRIVATE CREDENTIALS"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = privateRoot(filepath.Join(root, "materials")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "materials", "synthetic.txt"), []byte("KEEP MATERIAL"), 0600); err != nil {
		t.Fatal(err)
	}
	result, code := maintenance(context.Background(), root, []string{"backup"}, recoveryIO{})
	if code != "" {
		t.Fatal(code)
	}
	return root, result["backupId"].(string)
}
func mutateFixture(t *testing.T, root string, sidecars bool) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "workspace.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE tasks SET title='After backup';UPDATE workspace_meta SET workspace_revision=43"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if sidecars {
		for _, name := range []string{"workspace.sqlite-wal", "workspace.sqlite-shm"} {
			if err = os.WriteFile(filepath.Join(root, name), []byte("stale synthetic "+name), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func assertRestored(t *testing.T, root, backup, id string) {
	t.Helper()
	ctx := context.Background()
	c, err := migrations.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyExact(ctx, filepath.Join(root, "workspace.sqlite"), migrations.Latest, c); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(root, "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.LoadWorkspace(ctx)
	s.Close()
	if err != nil || snapshot.WorkspaceRevision != 42 || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Title != "Saved synthetic task" || snapshot.Tasks[0].Revision != 7 {
		t.Fatalf("saved facts lost: %+v %v", snapshot, err)
	}
	for name, want := range map[string]string{"credentials.json": "KEEP PRIVATE CREDENTIALS", "materials/synthetic.txt": "KEEP MATERIAL"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != want {
			t.Fatal("unrelated private data changed", name, err)
		}
	}
	if _, err = verifyBackup(ctx, root, backup, c); err != nil {
		t.Fatal("selected backup lost", err)
	}
	if err = recoveryPending(root); err != nil {
		t.Fatal("completion still blocked", err)
	}
	if _, err = os.Stat(filepath.Join(root, "recovery", id, "completed.json")); err != nil {
		t.Fatal("completion evidence lost", err)
	}
}
func TestMaintenanceBackupRestoreResumePreservesFactsAndPrivateFiles(t *testing.T) {
	root, backup := recoveryFixture(t)
	mutateFixture(t, root, true)
	result, code := maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{})
	if code != "" {
		t.Fatal(code)
	}
	id := result["restoreId"].(string)
	assertRestored(t, root, backup, id)
	for _, name := range []string{"workspace.sqlite", "workspace.sqlite-wal", "workspace.sqlite-shm"} {
		if _, err := os.Stat(filepath.Join(root, "recovery", id, name)); err != nil {
			t.Fatal("quarantined file lost", name, err)
		}
	}
	if _, code = maintenance(context.Background(), root, []string{"resume", id}, recoveryIO{}); code != "" {
		t.Fatal("repeat resume", code)
	}
}
func TestRestoreEveryDurableBoundaryResumesWithoutFreshDatabase(t *testing.T) {
	for _, boundary := range []string{"restore-intent", "move-workspace.sqlite", "quarantine-workspace.sqlite", "move-workspace.sqlite-wal", "quarantine-workspace.sqlite-wal", "move-workspace.sqlite-shm", "quarantine-workspace.sqlite-shm", "restore-install-move", "restore-installed", "restore-completion-move"} {
		t.Run(boundary, func(t *testing.T) {
			root, backup := recoveryFixture(t)
			mutateFixture(t, root, true)
			_, code := maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{boundary: func(name string) error {
				if name == boundary {
					return errors.New("injected interruption")
				}
				return nil
			}})
			if code == "" {
				t.Fatal("injection not reached")
			}
			c, _ := migrations.Embedded()
			var raw restoreIntent
			if err := readJSON(filepath.Join(root, "restore-intent.json"), &raw); err != nil {
				t.Fatal(err)
			}
			intent, err := loadIntent(root, raw.ID, c)
			if err != nil {
				t.Fatal(err)
			}
			before, presentErr := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
			w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
			if w != nil {
				w.Close()
			}
			var startup *StartupError
			if !errors.As(err, &startup) || startup.Code != "recovery_required" {
				t.Fatal("normal startup was not blocked", err)
			}
			after, afterErr := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
			if !reflect.DeepEqual(before, after) || errors.Is(presentErr, os.ErrNotExist) != errors.Is(afterErr, os.ErrNotExist) {
				t.Fatal("blocked startup created or changed database")
			}
			if _, code = maintenance(context.Background(), root, []string{"resume", intent.ID}, recoveryIO{}); code != "" {
				t.Fatal("resume", code)
			}
			assertRestored(t, root, backup, intent.ID)
		})
	}
}
func TestInterruptedRestoreProcessesAreKilledAndResume(t *testing.T) {
	for _, boundary := range []string{"restore-intent", "move-workspace.sqlite", "quarantine-workspace.sqlite", "move-workspace.sqlite-wal", "quarantine-workspace.sqlite-wal", "move-workspace.sqlite-shm", "quarantine-workspace.sqlite-shm", "restore-install-move", "restore-installed", "restore-completion-move", "restore-complete"} {
		t.Run(boundary, func(t *testing.T) {
			root, backup := recoveryFixture(t)
			mutateFixture(t, root, true)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRestoreKillHelper$")
			cmd.Env = append(os.Environ(), "PERSONAL_DESK_RESTORE_HELPER="+root, "PERSONAL_DESK_RESTORE_BACKUP="+backup, "PERSONAL_DESK_RESTORE_BOUNDARY="+boundary)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(stdout)
			if !scanner.Scan() || scanner.Text() != "READY" {
				_ = cmd.Wait()
				t.Fatalf("helper did not reach %s: %s", boundary, stderr.String())
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			var intent restoreIntent
			path := filepath.Join(root, "restore-intent.json")
			if boundary == "restore-complete" {
				entries, err := os.ReadDir(filepath.Join(root, "recovery"))
				if err != nil || len(entries) != 1 {
					t.Fatal(err)
				}
				path = filepath.Join(root, "recovery", entries[0].Name(), "completed.json")
			}
			if err = readJSON(path, &intent); err != nil {
				t.Fatal(err)
			}
			if _, code := maintenance(context.Background(), root, []string{"resume", intent.ID}, recoveryIO{}); code != "" {
				t.Fatal("killed-process resume", code)
			}
			assertRestored(t, root, backup, intent.ID)
		})
	}
}
func TestRestoreKillHelper(t *testing.T) {
	root := os.Getenv("PERSONAL_DESK_RESTORE_HELPER")
	if root == "" {
		return
	}
	_, code := maintenance(context.Background(), root, []string{"restore", os.Getenv("PERSONAL_DESK_RESTORE_BACKUP")}, recoveryIO{boundary: func(name string) error {
		if name == os.Getenv("PERSONAL_DESK_RESTORE_BOUNDARY") {
			fmt.Println("READY")
			time.Sleep(30 * time.Second)
			return errors.New("parent failed to kill helper")
		}
		return nil
	}})
	if code != "" {
		t.Fatal(code)
	}
}
func TestMalformedIntentBlocksBeforeSQLiteCreationAndReleasesLock(t *testing.T) {
	for _, data := range []string{"{", `{"format":1,"id":"../unsafe"}`, `{"format":1,"unknown":true}`} {
		root := newRoot(t)
		if err := privateRoot(root); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "restore-intent.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		w, err := Start(context.Background(), Config{Root: root}, diagnostics.New())
		if w != nil {
			w.Close()
		}
		var startup *StartupError
		if !errors.As(err, &startup) || startup.Code != "recovery_required" {
			t.Fatal(err)
		}
		if _, err = os.Stat(filepath.Join(root, "workspace.sqlite")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("created fresh database")
		}
		lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
		if err != nil {
			t.Fatal("ownership leaked", err)
		}
		releaseLock(lock)
	}
}
func TestRecoveryUnsafeIDCorruptBackupAndCollisionPreserveEvidence(t *testing.T) {
	for _, id := range []string{"../secret", "/tmp/file", strings.Repeat("a", 33), "-force"} {
		var out bytes.Buffer
		if RunMaintenance(context.Background(), newRoot(t), []string{"restore", id}, &out) != 1 || strings.Contains(out.String(), id) {
			t.Fatal("unsafe ID or raw output", id, out.String())
		}
	}
	t.Run("corrupt", func(t *testing.T) {
		root, backup := recoveryFixture(t)
		path := filepath.Join(root, "backups", backup+".sqlite")
		before, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
		if err := os.WriteFile(path, []byte("corrupt evidence"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, code := maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{}); code == "" {
			t.Fatal("accepted corrupt image")
		}
		after, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
		if !reflect.DeepEqual(before, after) {
			t.Fatal("corrupt backup replaced current")
		}
	})
	t.Run("collision", func(t *testing.T) {
		root, backup := recoveryFixture(t)
		mutateFixture(t, root, false)
		_, _ = maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{boundary: func(name string) error {
			if name == "restore-intent" {
				return errRecovery
			}
			return nil
		}})
		var intent restoreIntent
		_ = readJSON(filepath.Join(root, "restore-intent.json"), &intent)
		target := filepath.Join(root, "recovery", intent.ID, "workspace.sqlite")
		if err := os.WriteFile(target, []byte("KEEP COLLISION"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, code := maintenance(context.Background(), root, []string{"resume", intent.ID}, recoveryIO{}); code == "" {
			t.Fatal("overwrote collision")
		}
		data, _ := os.ReadFile(target)
		if string(data) != "KEEP COLLISION" {
			t.Fatal("collision evidence changed")
		}
	})
	t.Run("directory-alias", func(t *testing.T) {
		root, backup := recoveryFixture(t)
		if err := os.Rename(filepath.Join(root, "backups"), filepath.Join(root, "original-backups")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "original-backups"), filepath.Join(root, "backups")); err != nil {
			t.Fatal(err)
		}
		if _, code := maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{}); code == "" {
			t.Fatal("followed backup directory alias")
		}
	})
}
func TestUpgradeRetainsOriginalRecoveryPointAcrossPartialFailure(t *testing.T) {
	root, manual := recoveryFixture(t)
	second := "-- +goose Up\nCREATE TABLE future_notes(value TEXT);\n-- +goose Down\nDROP TABLE future_notes;\n"
	broken := "-- +goose Up\nCREATE TABLE rolled_back(value TEXT);\nINSERT INTO absent VALUES(1);\n-- +goose Down\nSELECT 1;\n"
	c, err := migrationfixture.Catalog(second, broken)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		w, err := startCatalog(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New(), c, recoveryIO{})
		if w != nil {
			w.Close()
		}
		if err == nil {
			t.Fatal("defective upgrade started API")
		}
	}
	backups, err := listBackups(context.Background(), root, c)
	if err != nil || len(backups) != 2 {
		t.Fatal("replaced or multiplied original recovery point", len(backups), err)
	}
	for _, m := range backups {
		if m.ID != manual && (m.Kind != "upgrade" || m.SchemaVersion != migrations.Latest) {
			t.Fatal(m)
		}
	}
	state, err := sqlite.Inspect(context.Background(), filepath.Join(root, "workspace.sqlite"), c)
	if err != nil || state.Version != migrations.Latest+1 {
		t.Fatal(state, err)
	}
	if _, err = os.Stat(filepath.Join(root, "discovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed upgrade advertised API")
	}
}
func TestRecoverySyncFailuresAndCancellationStopSafely(t *testing.T) {
	root, backup := recoveryFixture(t)
	mutateFixture(t, root, false)
	armed := false
	_, code := maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{boundary: func(name string) error {
		if name == "restore-intent" {
			armed = true
		}
		return nil
	}, syncDirectory: func(path string) error {
		if armed {
			return errors.New("injected directory sync failure")
		}
		return syncDir(path)
	}})
	if code == "" || recoveryPending(root) == nil {
		t.Fatal("sync failure lost blocking intent")
	}
	var intent restoreIntent
	_ = readJSON(filepath.Join(root, "restore-intent.json"), &intent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, code = maintenance(ctx, root, []string{"resume", intent.ID}, recoveryIO{}); code == "" {
		t.Fatal("cancelled resume succeeded")
	}
	if _, code = maintenance(context.Background(), root, []string{"resume", intent.ID}, recoveryIO{}); code != "" {
		t.Fatal(code)
	}
	assertRestored(t, root, backup, intent.ID)
}
func TestCompetingMaintenanceLeavesOwnerFilesUntouched(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	defer w.Close()
	before, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
	for _, args := range [][]string{{"backup"}, {"list-backups"}, {"restore", strings.Repeat("a", 32)}, {"resume", strings.Repeat("b", 32)}} {
		if _, code := maintenance(context.Background(), root, args, recoveryIO{}); code != "workspace_owned" {
			t.Fatal(code)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("competing maintenance changed owner database")
	}
}

func TestIncompatibleBackupAndMalformedRegistryStopBeforeReplacement(t *testing.T) {
	root, backup := recoveryFixture(t)
	c, err := migrationfixture.Catalog("-- +goose Up\nCREATE TABLE future_notes(value TEXT);\n-- +goose Down\nDROP TABLE future_notes;\n")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
	if _, err = prepareRestore(context.Background(), root, backup, c, recoveryIO{}); err == nil {
		t.Fatal("restored a schema that requires migration")
	}
	if err = os.WriteFile(filepath.Join(root, "backups", backup+".json"), []byte("{ malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := startCatalog(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New(), c, recoveryIO{})
	if w != nil {
		w.Close()
	}
	if err == nil {
		t.Fatal("invalid registry allowed upgrade")
	}
	after, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid recovery input changed current database")
	}
	if _, err = os.Lstat(filepath.Join(root, "restore-intent.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("incompatible restore published intent")
	}
}

func TestCompletedUpgradeReturnAndNewUpgradeRetainNewFacts(t *testing.T) {
	ctx := context.Background()
	root, _ := recoveryFixture(t)
	if err := os.Remove(filepath.Join(root, "credentials.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCredentials(filepath.Join(root, "credentials.json")); err != nil {
		t.Fatal(err)
	}
	c, err := migrationfixture.Catalog("-- +goose Up\nCREATE TABLE future_notes(value TEXT);\n-- +goose Down\nDROP TABLE future_notes;\n")
	if err != nil {
		t.Fatal(err)
	}
	w, err := startCatalog(ctx, Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New(), c, recoveryIO{})
	if err != nil {
		t.Fatal("first upgrade", err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	backups, err := listBackups(ctx, root, c)
	if err != nil {
		t.Fatal(err)
	}
	var original backupManifest
	for _, m := range backups {
		if m.Kind == "upgrade" {
			original = m
		}
	}
	if original.ID == "" {
		t.Fatal("missing original recovery point")
	}
	oldImage := filepath.Join(root, "backups", original.ID+".sqlite")
	oldHash, oldSize, err := digest(ctx, oldImage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(filepath.Join(root, "upgrade-intent.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("successful attempt still active", err)
	}
	// The compatible v1 executable performs its normal registered restore. A
	// later upgrade to the same chain is a new attempt with new saved facts.
	if _, code := maintenance(ctx, root, []string{"restore", original.ID}, recoveryIO{}); code != "" {
		t.Fatal("return to v1", code)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "workspace.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE tasks SET title='New fact after return',revision=8;UPDATE workspace_meta SET workspace_revision=43"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = startCatalog(ctx, Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New(), c, recoveryIO{})
	if err != nil {
		t.Fatal("second upgrade", err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	backups, err = listBackups(ctx, root, c)
	if err != nil {
		t.Fatal(err)
	}
	var newer backupManifest
	count := 0
	for _, m := range backups {
		if m.Kind == "upgrade" {
			count++
			if m.ID != original.ID {
				newer = m
			}
		}
	}
	if count != 2 || newer.ID == "" {
		t.Fatal("completed cycle reused stale recovery point", count)
	}
	if err = matches(ctx, oldImage, oldHash, oldSize); err != nil {
		t.Fatal("old recovery image changed", err)
	}
	image, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "backups", newer.ID+".sqlite"))+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	var title string
	var revision, workspaceRevision int
	if err = image.QueryRow("SELECT title,revision FROM tasks WHERE id='T-frozen'").Scan(&title, &revision); err != nil || title != "New fact after return" || revision != 8 {
		t.Fatal("new pre-upgrade facts missing", title, revision, err)
	}
	if err = image.QueryRow("SELECT workspace_revision FROM workspace_meta").Scan(&workspaceRevision); err != nil || workspaceRevision != 43 {
		t.Fatal("new content revision missing", workspaceRevision, err)
	}
}

func TestLateCompletionSyncFailuresRetainGateAndReconcilePair(t *testing.T) {
	for _, failure := range []string{"completion-directory", "root-before-retirement", "root-after-retirement"} {
		t.Run(failure, func(t *testing.T) {
			root, backup := recoveryFixture(t)
			mutateFixture(t, root, true)
			armed := false
			injected := false
			_, code := maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{boundary: func(name string) error {
				if name == "restore-completion-move" {
					armed = true
				}
				return nil
			}, syncDirectory: func(path string) error {
				pending, _ := exists(filepath.Join(root, "restore-intent.json"))
				shouldFail := failure == "completion-directory" && path != root || failure == "root-before-retirement" && path == root && pending || failure == "root-after-retirement" && path == root && !pending
				if armed && !injected && shouldFail {
					injected = true
					return errors.New("injected final completion sync failure")
				}
				return syncDir(path)
			}})
			if code == "" || !injected || recoveryPending(root) == nil {
				t.Fatal("late sync failure removed startup gate", code, injected)
			}
			var intent restoreIntent
			if err := readJSON(filepath.Join(root, "restore-intent.json"), &intent); err != nil {
				t.Fatal(err)
			}
			completed := filepath.Join(root, "recovery", intent.ID, "completed.json")
			before, err := os.ReadFile(completed)
			if err != nil {
				t.Fatal("completion evidence missing", err)
			}
			w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
			if w != nil {
				w.Close()
			}
			var startup *StartupError
			if !errors.As(err, &startup) || startup.Code != "recovery_required" {
				t.Fatal("normal startup escaped failed completion", err)
			}
			if _, code = maintenance(context.Background(), root, []string{"resume", intent.ID}, recoveryIO{}); code != "" {
				t.Fatal("pair could not resume", code)
			}
			after, _ := os.ReadFile(completed)
			if !bytes.Equal(before, after) {
				t.Fatal("completion evidence overwritten")
			}
			assertRestored(t, root, backup, intent.ID)
		})
	}
}

func TestRepeatedCompletionSyncFailuresPreserveLaterSavedWork(t *testing.T) {
	root, backup := recoveryFixture(t)
	result, code := maintenance(context.Background(), root, []string{"restore", backup}, recoveryIO{})
	if code != "" {
		t.Fatal(code)
	}
	id := result["restoreId"].(string)
	dir := filepath.Join(root, "recovery", id)
	completedPath := filepath.Join(dir, "completed.json")
	before, err := os.ReadFile(completedPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "workspace.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE tasks SET title='Later saved fact',revision=8;UPDATE workspace_meta SET workspace_revision=43"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	assertLaterWork := func(t *testing.T) {
		t.Helper()
		if _, err := os.Lstat(filepath.Join(root, "restore-intent.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("obsolete replacement intent was recreated", err)
		}
		store, err := sqlite.Open(context.Background(), filepath.Join(root, "workspace.sqlite"))
		if err != nil {
			t.Fatal("later work is no longer readable", err)
		}
		snapshot, err := store.LoadWorkspace(context.Background())
		closeErr := store.Close()
		if err != nil || closeErr != nil || snapshot.WorkspaceRevision != 43 || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Title != "Later saved fact" || snapshot.Tasks[0].Revision != 8 {
			t.Fatal("later saved facts/revisions changed", snapshot, err, closeErr)
		}
		after, err := os.ReadFile(completedPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("completion evidence changed", err)
		}
	}
	for _, failure := range []string{"setup-root", "completion-directory", "completion-root"} {
		t.Run(failure, func(t *testing.T) {
			injected := false
			seenCompletionDirectory := false
			_, code := maintenance(context.Background(), root, []string{"resume", id}, recoveryIO{syncDirectory: func(path string) error {
				if path == dir {
					seenCompletionDirectory = true
				}
				fail := failure == "setup-root" && path == root && !seenCompletionDirectory || failure == "completion-directory" && path == dir || failure == "completion-root" && path == root && seenCompletionDirectory
				if fail && !injected {
					injected = true
					return errors.New("repeated completion sync failure")
				}
				return syncDir(path)
			}})
			if code == "" || !injected {
				t.Fatal("repeated completion sync failure was not reported", code, injected)
			}
			assertLaterWork(t)
			var synced []string
			if _, code = maintenance(context.Background(), root, []string{"resume", id}, recoveryIO{syncDirectory: func(path string) error { synced = append(synced, path); return syncDir(path) }}); code != "" {
				t.Fatal("repeat completion retry", code)
			}
			if len(synced) < 3 || synced[len(synced)-2] != dir || synced[len(synced)-1] != root {
				t.Fatal("repeat completion omitted required syncs", synced)
			}
			assertLaterWork(t)
		})
	}
}
