//go:build darwin || linux

package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func defaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtimePlatform == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "PersonalDesk"), nil
	}
	return filepath.Join(home, ".local", "share", "PersonalDesk"), nil
}
func privateRoot(path string) error {
	// Mkdir is intentionally non-recursive: only the app-owned root is created.
	// Shared parent folders keep their existing ownership and permissions.
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Getuid()) {
		return errPrivate
	}
	return nil
}
func openPrivate(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		file.Close()
		return nil, errPrivate
	}
	return file, nil
}
func acquireLock(path string) (*os.File, error) {
	f, err := openPrivate(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errOwned
		}
		return nil, err
	}
	return f, nil
}
func releaseLock(f *os.File) error {
	return errors.Join(unix.Flock(int(f.Fd()), unix.LOCK_UN), f.Close())
}

// openPrivateIn combines pinned directory confinement with final-component
// no-follow and the same owner/mode/link checks as other service files.
func openPrivateIn(root *diagnosticDirectory, name string, flags int) (*os.File, error) {
	f, err := root.openFile(name, flags)
	if err != nil {
		return nil, err
	}
	if err := validateDiagnosticFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func validateDiagnosticFile(f *os.File) error {
	if f == nil {
		return errPrivate
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 || diagnosticAlias(f) {
		return errPrivate
	}
	return nil
}
