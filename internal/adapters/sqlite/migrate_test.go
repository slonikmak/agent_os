package sqlite

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"personaldesk/internal/testsupport/migrationfixture"
	"personaldesk/migrations"
)

const second = "-- +goose Up\nCREATE TABLE future_notes(id TEXT PRIMARY KEY, note TEXT NOT NULL);\n-- +goose Down\nDROP TABLE future_notes;\n"
const third = "-- +goose Up\nALTER TABLE future_notes ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;\n-- +goose Down\nSELECT 1;\n"

func frozen(t *testing.T) string {
	t.Helper()
	path := database(t)
	if err := migrationfixture.Populate(path); err != nil {
		t.Fatal(err)
	}
	return path
}
func catalog(t *testing.T, scripts ...string) *migrations.Catalog {
	t.Helper()
	c, err := migrationfixture.Catalog(scripts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func assertSaved(t *testing.T, path string) {
	t.Helper()
	db, err := singleDB(path, "ro", true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title string
	var revision, workspaceRevision int
	if err = db.QueryRow("SELECT title,revision FROM tasks WHERE id='T-frozen'").Scan(&title, &revision); err != nil || title != "Saved synthetic task" || revision != 7 {
		t.Fatalf("lost fact: %s %d %v", title, revision, err)
	}
	if err = db.QueryRow("SELECT workspace_revision FROM workspace_meta").Scan(&workspaceRevision); err != nil || workspaceRevision != 42 {
		t.Fatalf("changed content revision: %d %v", workspaceRevision, err)
	}
}
func TestFrozenV1UpgradeSkippedReleaseAndCurrentNoBackup(t *testing.T) {
	ctx := context.Background()
	path := frozen(t)
	c := catalog(t, second, third)
	calls := 0
	s, err := OpenCatalog(ctx, path, c, func(ctx context.Context, state State) error {
		calls++
		if state.Version != 1 {
			t.Fatal(state)
		}
		dest := database(t)
		_, err := Backup(ctx, path, dest, c)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	assertSaved(t, path)
	state, err := Inspect(ctx, path, c)
	if err != nil || state.Version != migrations.Latest+2 || state.Pending {
		t.Fatal(state, err)
	}
	s, err = OpenCatalog(ctx, path, c, func(context.Context, State) error { calls++; return errors.New("must not back up current") })
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestMigrationStepFailureRetryKeepsHonestHistoryAndBackup(t *testing.T) {
	ctx := context.Background()
	path := frozen(t)
	broken := "-- +goose Up\nCREATE TABLE rolled_back(value TEXT);\nINSERT INTO absent_table VALUES('broken');\n-- +goose Down\nSELECT 1;\n"
	c := catalog(t, second, broken)
	backup := database(t)
	calls := 0
	before := func(ctx context.Context, state State) error {
		calls++
		if calls == 1 {
			_, err := Backup(ctx, path, backup, c)
			return err
		}
		return nil
	}
	for range 2 {
		s, err := OpenCatalog(ctx, path, c, before)
		if s != nil {
			s.Close()
		}
		if err == nil {
			t.Fatal("defective pending SQL was skipped")
		}
		state, err := Inspect(ctx, path, c)
		if err != nil || state.Version != migrations.Latest+1 {
			t.Fatal(state, err)
		}
		assertSaved(t, path)
	}
	db, _ := singleDB(path, "ro", true)
	defer db.Close()
	var count int
	_ = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='rolled_back'").Scan(&count)
	if count != 0 {
		t.Fatal("failed DDL committed")
	}
	state, err := Inspect(ctx, backup, c)
	if err != nil || state.Version != 1 {
		t.Fatal("original backup lost", state, err)
	}
	assertSaved(t, backup)
}
func TestBackupFailurePrecedesAnyDDLAndCancelledStepRollsBack(t *testing.T) {
	path := frozen(t)
	before, _ := os.ReadFile(path)
	c := catalog(t, second)
	if s, err := OpenCatalog(context.Background(), path, c, func(context.Context, State) error { return errors.New("backup sync failed") }); err == nil {
		s.Close()
		t.Fatal("migration ignored backup failure")
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("DDL before backup")
	}
	long := "-- +goose Up\nCREATE TABLE cancelled_step(value INTEGER);\nINSERT INTO cancelled_step WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT x FROM n;\n-- +goose Down\nSELECT 1;\n"
	c = catalog(t, long)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if s, err := OpenCatalog(ctx, path, c, func(context.Context, State) error { return nil }); err == nil {
		s.Close()
		t.Fatal("cancelled migration succeeded")
	}
	state, err := Inspect(context.Background(), path, c)
	// Cancellation can occur between any two individually committed migration
	// steps. Every recognized prefix before the long step is a valid outcome.
	if err != nil || state.Version < 1 || state.Version > migrations.Latest || !state.Pending {
		t.Fatal(state, err)
	}
	db, err := singleDB(path, "ro", true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cancelledTables int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='cancelled_step'`).Scan(&cancelledTables); err != nil || cancelledTables != 0 {
		t.Fatal("cancelled step leaked DDL", err)
	}
	assertSaved(t, path)
}
func TestBackupIncludesRealCommittedWAL(t *testing.T) {
	path := frozen(t)
	db, err := singleDB(path, "rw", false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("PRAGMA wal_autocheckpoint=0; UPDATE tasks SET title='Committed WAL fact'; UPDATE workspace_meta SET workspace_revision=43"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatal("test did not leave committed WAL", err)
	}
	destination := database(t)
	c := catalog(t)
	state, err := Backup(context.Background(), path, destination, c)
	if err != nil || state.Version != 1 {
		t.Fatal(state, err)
	}
	copy, err := singleDB(destination, "ro", true)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var title string
	if err = copy.QueryRow("SELECT title FROM tasks").Scan(&title); err != nil || title != "Committed WAL fact" {
		t.Fatal(title, err)
	}
}
func TestHistoryAndSchemaDriftRefusedBeforeBackup(t *testing.T) {
	for name, statement := range map[string]string{"missing": "DELETE FROM goose_db_version WHERE version_id=0", "newer": "INSERT INTO goose_db_version(version_id,is_applied) VALUES(9,1)", "down": "INSERT INTO goose_db_version(version_id,is_applied) VALUES(1,0)", "duplicate": "INSERT INTO goose_db_version(version_id,is_applied) VALUES(1,1)", "non-prefix": "INSERT INTO goose_db_version(version_id,is_applied) VALUES(3,1)", "schema": "ALTER TABLE tasks ADD COLUMN surprise TEXT"} {
		t.Run(name, func(t *testing.T) {
			path := frozen(t)
			db, _ := singleDB(path, "rw", false)
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			db.Close()
			before, _ := os.ReadFile(path)
			called := false
			if s, err := OpenCatalog(context.Background(), path, catalog(t, second, third), func(context.Context, State) error { called = true; return nil }); !errors.Is(err, ErrUnsupported) {
				if s != nil {
					s.Close()
				}
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if called || !reflect.DeepEqual(before, after) {
				t.Fatal("rejection touched database or backed it up")
			}
		})
	}
}
func TestBackupCancellationAndInvalidDestination(t *testing.T) {
	path := frozen(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Backup(ctx, path, database(t), catalog(t)); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	if _, err := Backup(context.Background(), path, filepath.Join(t.TempDir(), "absent", "image"), catalog(t)); err == nil {
		t.Fatal("invalid destination succeeded")
	}
}

const interruptedStep = "-- +goose Up\nCREATE TABLE interrupted_step(value INTEGER);\nINSERT INTO interrupted_step WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT x FROM n;\n-- +goose Down\nSELECT 1;\n"

func TestKilledMigrationKeepsEarlierCommitAndRollsBackInterruptedStep(t *testing.T) {
	path := frozen(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMigrationKillHelper$")
	cmd.Env = append(os.Environ(), "PERSONAL_DESK_MIGRATION_HELPER="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "READY" {
		t.Fatal("migration helper not ready")
	}
	c := catalog(t, second, interruptedStep)
	deadline := time.Now().Add(3 * time.Second)
	committed := false
	for time.Now().Before(deadline) {
		state, err := Inspect(ctx, path, c)
		if err == nil && state.Version == migrations.Latest+1 {
			committed = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !committed {
		t.Fatal("earlier migration did not commit before interruption")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	state, err := Inspect(context.Background(), path, c)
	if err != nil || state.Version != migrations.Latest+1 {
		t.Fatal("interrupted step changed history", state, err)
	}
	assertSaved(t, path)
	db, _ := singleDB(path, "ro", true)
	defer db.Close()
	var n int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='interrupted_step'").Scan(&n); err != nil || n != 0 {
		t.Fatal("interrupted DDL survived", n, err)
	}
}
func TestMigrationKillHelper(t *testing.T) {
	path := os.Getenv("PERSONAL_DESK_MIGRATION_HELPER")
	if path == "" {
		return
	}
	c := catalog(t, second, interruptedStep)
	s, err := OpenCatalog(context.Background(), path, c, func(context.Context, State) error { fmt.Println("READY"); return nil })
	if s != nil {
		s.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestFailedFirstInitializationRetriesWithPreservedBaseline(t *testing.T) {
	for _, failure := range []string{"sql-failure", "cancellation"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			path := database(t)
			c := catalog(t)
			writer, err := singleDB(path, "rw", false)
			if err != nil {
				t.Fatal(err)
			}
			p, err := provider(writer, c)
			if err != nil {
				writer.Close()
				t.Fatal(err)
			}
			if version, err := p.GetDBVersion(ctx); err != nil || version != 0 {
				writer.Close()
				t.Fatal(version, err)
			}
			var id int64
			var timestamp string
			if err = writer.QueryRow("SELECT id,tstamp FROM goose_db_version WHERE version_id=0").Scan(&id, &timestamp); err != nil {
				writer.Close()
				t.Fatal(err)
			}
			attempt := ctx
			if failure == "sql-failure" {
				// A real SQLite write failure after Goose's separately committed
				// baseline exercises the same provider used by OpenCatalog.
				if _, err = writer.Exec("PRAGMA query_only=ON"); err != nil {
					writer.Close()
					t.Fatal(err)
				}
			} else {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				attempt = cancelled
			}
			if _, err = p.Up(attempt); err == nil {
				writer.Close()
				t.Fatal("first initialization did not fail")
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			state, err := Inspect(ctx, path, c)
			if err != nil || state != (State{Fresh: true, Version: 0, Pending: true}) {
				t.Fatalf("baseline retry state: %+v %v", state, err)
			}
			s, err := Open(ctx, path)
			if err != nil {
				t.Fatal("first initialization could not resume", err)
			}
			defer s.Close()
			var gotID, applied, count int64
			var gotTimestamp string
			if err = s.writer.QueryRow("SELECT id,is_applied,tstamp FROM goose_db_version WHERE version_id=0").Scan(&gotID, &applied, &gotTimestamp); err != nil {
				t.Fatal(err)
			}
			if gotID != id || applied != 1 || gotTimestamp != timestamp {
				t.Fatal("retry reset the original baseline")
			}
			if err = s.writer.QueryRow("SELECT count(*) FROM goose_db_version").Scan(&count); err != nil || count != migrations.Latest+1 {
				t.Fatal("retry duplicated or skipped schema history", count, err)
			}
			if snapshot, err := s.LoadWorkspace(ctx); err != nil || snapshot.WorkspaceRevision != 1 || len(snapshot.Tasks) != 0 {
				t.Fatal("resumed initialization not ready", snapshot, err)
			}
		})
	}
}

func TestBaselineOnlyHistoryRequiresExactSchemaAndBaseline(t *testing.T) {
	for _, statement := range []string{"CREATE TABLE unrelated(value TEXT)", "DELETE FROM goose_db_version", "UPDATE goose_db_version SET is_applied=0", "ALTER TABLE goose_db_version ADD COLUMN unexpected TEXT"} {
		t.Run(statement, func(t *testing.T) {
			ctx := context.Background()
			path := database(t)
			c := catalog(t)
			writer, err := singleDB(path, "rw", false)
			if err != nil {
				t.Fatal(err)
			}
			p, err := provider(writer, c)
			if err != nil {
				writer.Close()
				t.Fatal(err)
			}
			if _, err = p.GetDBVersion(ctx); err != nil {
				writer.Close()
				t.Fatal(err)
			}
			if _, err = writer.Exec(statement); err != nil {
				writer.Close()
				t.Fatal(err)
			}
			writer.Close()
			before, _ := os.ReadFile(path)
			if _, err = Inspect(ctx, path, c); !errors.Is(err, ErrUnsupported) {
				t.Fatal("unrecognised baseline accepted", err)
			}
			after, _ := os.ReadFile(path)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("baseline refusal changed source")
			}
		})
	}
}

func TestMigrationObserverIsTypedAndCannotChangeMigrationResult(t *testing.T) {
	path := database(t)
	c, err := migrations.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	var stages []MigrationStage
	s, err := OpenCatalogObserved(context.Background(), path, c, nil, func(stage MigrationStage, duration time.Duration, ok bool) {
		stages = append(stages, stage)
		if duration < 0 || !ok {
			t.Fatal("wrong technical stage")
		}
		panic("SECRET driver SQL /private")
	})
	if err != nil {
		t.Fatal("diagnostic observer changed migration", err)
	}
	defer s.Close()
	if !reflect.DeepEqual(stages, []MigrationStage{StageInspect, StageApply, StageVerify}) {
		t.Fatal("missing migration stages", stages)
	}
	if _, err = s.LoadWorkspace(context.Background()); err != nil {
		t.Fatal(err)
	}
}
