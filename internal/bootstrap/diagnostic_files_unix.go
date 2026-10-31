//go:build darwin || linux

package bootstrap

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// diagnosticDirectory pins the logs directory exactly as the materials adapter
// pins content. Every ancestor is opened no-follow from a filesystem handle;
// path replacement cannot redirect any subsequent append/rotation/marker write.
type diagnosticDirectory struct{ *os.File }

func openDiagnosticDirectory(root string) (*diagnosticDirectory, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errPrivate
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "anchor")
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		current.Close()
		if e != nil {
			return nil, e
		}
		current = os.NewFile(uintptr(next), "anchor")
		if diagnosticAlias(current) {
			current.Close()
			return nil, errPrivate
		}
	}
	defer current.Close()
	if !privateDiagnosticDirectory(current) {
		return nil, errPrivate
	}
	if err = unix.Mkdirat(int(current.Fd()), "logs", 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, err
	}
	fd, err = unix.Openat(int(current.Fd()), "logs", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), "logs")
	if !privateDiagnosticDirectory(dir) || diagnosticAlias(dir) {
		dir.Close()
		return nil, errPrivate
	}
	if err = dir.Sync(); err == nil {
		err = current.Sync()
	}
	if err != nil {
		dir.Close()
		return nil, err
	}
	return &diagnosticDirectory{dir}, nil
}
func privateDiagnosticDirectory(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.IsDir() && info.Mode().Perm() == 0700 && stat.Uid == uint32(os.Getuid())
}
func (d *diagnosticDirectory) openFile(name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(d.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "diagnostic"), nil
}
func (d *diagnosticDirectory) Remove(name string) error { return unix.Unlinkat(int(d.Fd()), name, 0) }
func (d *diagnosticDirectory) Rename(old, new string) error {
	return unix.Renameat(int(d.Fd()), old, int(d.Fd()), new)
}
