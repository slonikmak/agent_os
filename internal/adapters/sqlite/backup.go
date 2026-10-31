package sqlite

import (
	"context"
	"errors"
	"os"

	driver "modernc.org/sqlite"
	"personaldesk/migrations"
)

// Backup writes to an already-created private, empty destination owned by the
// caller. The driver backup API includes committed WAL pages. Each bounded
// step observes cancellation; Finish executes exactly once on every path.
func Backup(ctx context.Context, source, destination string, catalog *migrations.Catalog) (State, error) {
	info, err := os.Lstat(destination)
	if err != nil {
		return State{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != 0 {
		return State{}, ErrUnsupported
	}
	state, err := Inspect(ctx, source, catalog)
	if err != nil || state.Fresh {
		return State{}, errors.Join(ErrUnsupported, err)
	}
	db, err := singleDB(source, "ro", true)
	if err != nil {
		return State{}, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return State{}, err
	}
	defer conn.Close()
	err = conn.Raw(func(raw any) (result error) {
		api, ok := raw.(interface {
			NewBackup(string) (*driver.Backup, error)
		})
		if !ok {
			return ErrUnsupported
		}
		backup, err := api.NewBackup(dsn(destination, "rw", false))
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, backup.Finish()) }()
		for {
			if err = ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(64)
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	})
	if err != nil {
		return State{}, err
	}
	verified, err := InspectImage(ctx, destination, catalog)
	if err != nil || verified != state {
		return State{}, errors.Join(ErrUnsupported, err)
	}
	f, err := os.OpenFile(destination, os.O_RDWR, 0)
	if err != nil {
		return State{}, err
	}
	err = errors.Join(f.Sync(), f.Close())
	return verified, err
}
