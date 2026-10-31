package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func database(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}
func TestFreshRestartAndReadOnlyPools(t *testing.T) {
	ctx := context.Background()
	path := database(t)
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.LoadWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if saved.WorkspaceID == "" || saved.WorkspaceRevision != 1 || saved.CreatedAt == "" || len(saved.Tasks) != 0 || saved.Tasks == nil || saved.Inbox == nil || saved.Sources == nil || saved.Counts.Tasks != 0 || saved.Counts.Inbox != 0 {
		t.Fatalf("not genuinely empty: %+v", saved)
	}
	var conns []*sql.Conn
	for range 4 {
		conn, err := store.readers.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		var fk, readonly, busy int
		for query, dest := range map[string]*int{"PRAGMA foreign_keys": &fk, "PRAGMA query_only": &readonly, "PRAGMA busy_timeout": &busy} {
			if err = conn.QueryRowContext(ctx, query).Scan(dest); err != nil {
				t.Fatal(err)
			}
		}
		if fk != 1 || readonly != 1 || busy != 2500 {
			t.Fatalf("unsafe pooled pragmas %d,%d,%d", fk, readonly, busy)
		}
		if _, err = conn.ExecContext(ctx, "UPDATE workspace_meta SET workspace_revision=2"); err == nil {
			t.Fatal("reader wrote canonical state")
		}
	}
	for _, conn := range conns {
		conn.Close()
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restarted, err := store.LoadWorkspace(ctx)
	if err != nil || !reflect.DeepEqual(saved, restarted) {
		t.Fatalf("restart changed state: %+v %v", restarted, err)
	}
}
func TestUnsupportedDatabasesStayUnchanged(t *testing.T) {
	for _, setup := range []string{
		"CREATE TABLE legacy(secret TEXT); INSERT INTO legacy VALUES ('keep')",
		"CREATE TABLE goose_db_version(id INTEGER PRIMARY KEY,version_id INTEGER,is_applied INTEGER); INSERT INTO goose_db_version VALUES(1,0,1),(2,99,1)",
		"CREATE TABLE goose_db_version(id INTEGER PRIMARY KEY,version_id INTEGER,is_applied INTEGER); INSERT INTO goose_db_version VALUES(1,0,1)",
		"CREATE TABLE goose_db_version(id INTEGER PRIMARY KEY,version_id INTEGER,is_applied INTEGER); INSERT INTO goose_db_version VALUES(1,1,1)",
	} {
		t.Run(setup[:min(30, len(setup))], func(t *testing.T) {
			path := database(t)
			db, err := sql.Open("sqlite", dsn(path, "rw", false))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(setup); err != nil {
				t.Fatal(err)
			}
			db.Close()
			before, _ := os.ReadFile(path)
			if store, err := Open(context.Background(), path); !errors.Is(err, ErrUnsupported) {
				if store != nil {
					store.Close()
				}
				t.Fatalf("error=%v", err)
			}
			after, _ := os.ReadFile(path)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("unsupported database was changed")
			}
		})
	}
}
func TestSnapshotCountsShareReadTransaction(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, database(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var wg sync.WaitGroup
	fail := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := int64(2); n < 80; n++ {
			tx, err := store.writer.BeginTx(ctx, nil)
			if err != nil {
				fail <- err
				return
			}
			if _, err = tx.Exec("INSERT INTO tasks(id,kind,status,title,revision,updated_at) VALUES(?, 'request','new','test',1,'2026-10-29T00:00:00Z')", n); err == nil {
				_, err = tx.Exec("UPDATE workspace_meta SET workspace_revision=?", n)
			}
			if err != nil {
				tx.Rollback()
				fail <- err
				return
			}
			if err = tx.Commit(); err != nil {
				fail <- err
				return
			}
		}
	}()
	for range 80 {
		snapshot, err := store.LoadWorkspace(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(snapshot.Tasks)) != snapshot.Counts.Tasks || snapshot.Counts.Tasks != snapshot.WorkspaceRevision-1 {
			t.Fatalf("torn snapshot: revision=%d count=%d rows=%d", snapshot.WorkspaceRevision, snapshot.Counts.Tasks, len(snapshot.Tasks))
		}
	}
	wg.Wait()
	select {
	case err := <-fail:
		t.Fatal(err)
	default:
	}
	cancelled, cancel := context.WithTimeout(ctx, time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err = store.LoadWorkspace(cancelled); err == nil {
		t.Fatal("cancelled query succeeded")
	}
}

func TestUnknownSchemaObjectsAreRefusedWithoutHistoryChanges(t *testing.T) {
	ctx := context.Background()
	path := database(t)
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.writer.Exec("CREATE TABLE future_state(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	before, _ := os.ReadFile(path)
	if reopened, err := Open(ctx, path); !errors.Is(err, ErrUnsupported) {
		if reopened != nil {
			reopened.Close()
		}
		t.Fatalf("unknown schema accepted: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unknown schema was altered")
	}
}

func TestLegalSQLitePrefixObjectsAreRefusedWithoutChanges(t *testing.T) {
	for _, test := range []struct {
		name        string
		initialized bool
		setup       string
	}{
		{"sole_table", false, "CREATE TABLE sqliteLegacy(value TEXT); INSERT INTO sqliteLegacy VALUES ('keep')"},
		{"current_table", true, "CREATE TABLE sqliteLegacy(value TEXT)"},
		{"current_view", true, "CREATE VIEW sqliteLegacyView AS SELECT 1"},
		{"current_index", true, "CREATE INDEX sqliteLegacyIndex ON tasks(title)"},
		{"current_trigger", true, "CREATE TRIGGER sqliteLegacyTrigger AFTER INSERT ON tasks BEGIN SELECT 1; END"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			path := database(t)
			if test.initialized {
				store, err := Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				if err = store.Close(); err != nil {
					t.Fatal(err)
				}
			}
			db, err := sql.Open("sqlite", dsn(path, "rw", false))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.ExecContext(ctx, test.setup); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if store, err := Open(ctx, path); !errors.Is(err, ErrUnsupported) {
				if store != nil {
					store.Close()
				}
				t.Fatalf("legal unknown sqlite prefix object accepted: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("unknown database journal or history was changed")
			}
		})
	}
}
