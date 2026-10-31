package bootstrap

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func moveNoReplace(from, to string) error {
	return unix.RenamexNp(from, to, unix.RENAME_EXCL|unix.RENAME_NOFOLLOW_ANY)
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
