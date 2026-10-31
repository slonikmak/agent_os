package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	"personaldesk/migrations"
)

type State struct {
	Fresh   bool
	Version int64
	Pending bool
}
type BeforeUpgrade func(context.Context, State) error

// MigrationStage is a closed technical inventory, never driver text or SQL.
type MigrationStage string

const (
	StageInspect MigrationStage = "inspect"
	StageBackup  MigrationStage = "backup"
	StageApply   MigrationStage = "apply"
	StageVerify  MigrationStage = "verify"
)

type MigrationObserver func(MigrationStage, time.Duration, bool)

func observe(observer MigrationObserver, stage MigrationStage, started time.Time, ok bool) {
	if observer != nil {
		func() { defer func() { _ = recover() }(); observer(stage, time.Since(started), ok) }()
	}
}

func provider(db *sql.DB, catalog *migrations.Catalog) (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectSQLite3, db, catalog.Files(), goose.WithDisableGlobalRegistry(true), goose.WithAllowOutofOrder(false), goose.WithIsolateDDL(false), goose.WithLogger(goose.NopLogger()))
}
func singleDB(path, mode string, readonly bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn(path, mode, readonly))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

// Inspect never constructs a provider against the inspected database. In
// particular an unknown database cannot acquire a Goose ledger as a side effect.
func Inspect(ctx context.Context, path string, catalog *migrations.Catalog) (State, error) {
	db, err := singleDB(path, "ro", true)
	if err != nil {
		return State{}, err
	}
	defer db.Close()
	return inspect(ctx, db, catalog)
}

// InspectImage validates a closed, self-contained recovery image. immutable
// suppresses SQLite's creation of WAL/SHM beside validation copies. Live sources
// must use Inspect instead, so their committed WAL records remain visible.
func InspectImage(ctx context.Context, path string, catalog *migrations.Catalog) (State, error) {
	u, err := url.Parse(dsn(path, "ro", true))
	if err != nil {
		return State{}, err
	}
	q := u.Query()
	q.Set("immutable", "1")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return State{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return inspect(ctx, db, catalog)
}

type schemaObject struct{ Kind, Name, Table, SQL string }

func schema(ctx context.Context, db *sql.DB) ([]schemaObject, error) {
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_schema WHERE substr(name,1,7) != 'sqlite_' ORDER BY type,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var objects []schemaObject
	for rows.Next() {
		var o schemaObject
		if err := rows.Scan(&o.Kind, &o.Name, &o.Table, &o.SQL); err != nil {
			return nil, err
		}
		o.SQL = strings.Join(strings.Fields(o.SQL), " ")
		objects = append(objects, o)
	}
	return objects, rows.Err()
}
func integrity(ctx context.Context, db *sql.DB) error {
	var value string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&value); err != nil || value != "ok" {
		return ErrUnsupported
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrUnsupported
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		return ErrUnsupported
	}
	return nil
}
func inspect(ctx context.Context, db *sql.DB, catalog *migrations.Catalog) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if catalog == nil {
		return State{}, migrations.ErrCatalog
	}
	objects, err := schema(ctx, db)
	if err != nil {
		return State{}, ErrUnsupported
	}
	if err = integrity(ctx, db); err != nil {
		return State{}, err
	}
	if len(objects) == 0 {
		return State{Fresh: true, Pending: true}, nil
	}
	rows, err := db.QueryContext(ctx, "SELECT id,version_id,is_applied FROM goose_db_version ORDER BY id")
	if err != nil {
		return State{}, ErrUnsupported
	}
	entries := catalog.Entries()
	n := -1
	var version, previousID int64
	for rows.Next() {
		var id, applied, v int64
		if err = rows.Scan(&id, &v, &applied); err != nil || id <= previousID || applied != 1 || n >= len(entries) {
			rows.Close()
			return State{}, ErrUnsupported
		}
		if n == -1 {
			if v != 0 {
				rows.Close()
				return State{}, ErrUnsupported
			}
		} else if v != entries[n].Version {
			rows.Close()
			return State{}, ErrUnsupported
		}
		previousID = id
		version = v
		n++
	}
	if err = closeRows(rows); err != nil || n < 0 {
		return State{}, ErrUnsupported
	}
	// Rebuild only the recognised prefix in an isolated ephemeral database.
	// Comparing complete schema definitions catches extra objects, missing
	// columns/constraints and drift without maintaining a second schema ledger.
	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return State{}, err
	}
	defer reference.Close()
	reference.SetMaxOpenConns(1)
	p, err := provider(reference, catalog)
	if err != nil {
		return State{}, err
	}
	if version == 0 {
		// UpTo rejects zero. Initialize only Goose's required metadata in the
		// isolated reference; no application migration belongs to this prefix.
		if _, err = p.GetDBVersion(ctx); err != nil {
			return State{}, err
		}
	} else if _, err = p.UpTo(ctx, version); err != nil {
		return State{}, err
	}
	want, err := schema(ctx, reference)
	if err != nil {
		return State{}, err
	}
	if !reflect.DeepEqual(objects, want) {
		return State{}, ErrUnsupported
	}
	// Goose can commit its baseline before the first SQL migration fails or is
	// interrupted. Only the exact baseline-only schema remains initialization:
	// there is no committed application state to back up, and the existing zero
	// history must be reused rather than reset on retry.
	return State{Fresh: version == 0, Version: version, Pending: version != catalog.Latest()}, nil
}

func Open(ctx context.Context, path string) (*Store, error) {
	catalog, err := migrations.Embedded()
	if err != nil {
		return nil, err
	}
	return OpenCatalog(ctx, path, catalog, nil)
}

// OpenCatalog requires caller-held workspace ownership. A pending upgrade of
// saved work cannot run without a successful durable backup callback.
func OpenCatalog(ctx context.Context, path string, catalog *migrations.Catalog, before BeforeUpgrade) (_ *Store, result error) {
	return OpenCatalogObserved(ctx, path, catalog, before, nil)
}
func OpenCatalogObserved(ctx context.Context, path string, catalog *migrations.Catalog, before BeforeUpgrade, observer MigrationObserver) (_ *Store, result error) {
	started := time.Now()
	state, err := Inspect(ctx, path, catalog)
	observe(observer, StageInspect, started, err == nil)
	if err != nil {
		return nil, err
	}
	if !state.Fresh && state.Pending {
		if before == nil {
			return nil, errors.New("verified backup required")
		}
		started = time.Now()
		err = before(ctx, state)
		observe(observer, StageBackup, started, err == nil)
		if err != nil {
			return nil, err
		}
	}
	writer, err := singleDB(path, "rw", false)
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			_ = writer.Close()
		}
	}()
	if state.Pending {
		if _, err = writer.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
			return nil, err
		}
		if _, err = writer.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
			return nil, err
		}
		p, err := provider(writer, catalog)
		if err != nil {
			return nil, err
		}
		started = time.Now()
		_, err = p.Up(ctx)
		observe(observer, StageApply, started, err == nil)
		if err != nil {
			return nil, err
		}
	}
	var journal string
	if err = writer.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		return nil, ErrUnsupported
	}
	if _, err = writer.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		return nil, err
	}
	started = time.Now()
	verified, err := inspect(ctx, writer, catalog)
	observe(observer, StageVerify, started, err == nil && !verified.Fresh && !verified.Pending)
	if err != nil {
		return nil, err
	}
	if verified.Fresh || verified.Pending {
		return nil, ErrUnsupported
	}
	readers, err := sql.Open("sqlite", dsn(path, "ro", true))
	if err != nil {
		return nil, err
	}
	readers.SetMaxOpenConns(4)
	readers.SetMaxIdleConns(4)
	store := &Store{writer: writer, readers: readers, writeGate: make(chan struct{}, 1)}
	if _, err = store.LoadWorkspace(ctx); err != nil {
		_ = readers.Close()
		return nil, err
	}
	return store, nil
}
