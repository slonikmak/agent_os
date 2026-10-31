//go:build darwin || linux

package bootstrap

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"personaldesk/internal/adapters/diagnostics"
)

func openExportParent(path string, protected []protectedDestination) (*os.File, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "export-parent")
	check := func(f *os.File) error {
		if diagnosticAlias(f) {
			return errPrivate
		}
		info, err := f.Stat()
		if err != nil {
			return err
		}
		for _, root := range protected {
			if root.info != nil && os.SameFile(info, root.info) {
				return errPrivate
			}
		}
		return nil
	}
	if err = check(current); err != nil {
		current.Close()
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		current.Close()
		if e != nil {
			return nil, e
		}
		current = os.NewFile(uintptr(next), "export-parent")
		if err = check(current); err != nil {
			current.Close()
			return nil, err
		}
	}
	return current, nil
}
func sameExportIdentity(dir *os.File, name string, original *unix.Stat_t) bool {
	var now unix.Stat_t
	return unix.Fstatat(int(dir.Fd()), name, &now, unix.AT_SYMLINK_NOFOLLOW) == nil && original.Dev == now.Dev && original.Ino == now.Ino
}

// The private staging directory is itself retained by handle: replacing its name
// in a shared Save parent cannot redirect the archive source. Keep the archive
// descriptor through no-replace publication and verify both names against it.
func publishDiagnosticArchive(ctx context.Context, path string, protected []protectedDestination, write func(io.Writer) error) error {
	return publishDiagnosticArchiveWithSync(ctx, path, protected, write, (*os.File).Sync)
}

// The small sync seam exercises failure/panic cleanup after atomic publication.
func publishDiagnosticArchiveWithSync(ctx context.Context, path string, protected []protectedDestination, write func(io.Writer) error, syncFile func(*os.File) error) error {
	return publishDiagnosticArchiveWithIO(ctx, path, protected, write, syncFile, unix.Openat)
}

// Opening is injectable only to exercise ownership of a just-created directory
// when descriptor allocation fails before a retained handle can be obtained.
func publishDiagnosticArchiveWithIO(ctx context.Context, path string, protected []protectedDestination, write func(io.Writer) error, syncFile func(*os.File) error, openStage func(int, string, int, uint32) (int, error)) (result error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.TrimSpace(path) != path || strings.ToLower(filepath.Ext(path)) != ".zip" {
		return errPrivate
	}
	for _, root := range protected {
		p, r := strings.ToLower(path), strings.ToLower(root.path)
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return errPrivate
		}
	}
	parent, err := openExportParent(path, protected)
	if err != nil {
		return err
	}
	defer parent.Close()
	leaf := filepath.Base(path)
	var existing unix.Stat_t
	if err = unix.Fstatat(int(parent.Fd()), leaf, &existing, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return errPrivate
	}
	temp := ".personaldesk-export-" + diagnostics.ID() + ".tmp"
	if err = unix.Mkdirat(int(parent.Fd()), temp, 0700); err != nil {
		return err
	}
	// Own the empty directory immediately, including when the following open or
	// validation fails. Never remove an independently replaced name.
	var stageIdentity unix.Stat_t
	stageKnown := false
	defer func() {
		if stageKnown && sameExportIdentity(parent, temp, &stageIdentity) {
			_ = unix.Unlinkat(int(parent.Fd()), temp, unix.AT_REMOVEDIR)
		}
	}()
	if err = unix.Fstatat(int(parent.Fd()), temp, &stageIdentity, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	stageKnown = true
	fd, err := openStage(int(parent.Fd()), temp, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	staging := os.NewFile(uintptr(fd), "export-staging")
	defer staging.Close()
	var openedIdentity unix.Stat_t
	if err = unix.Fstat(fd, &openedIdentity); err != nil {
		return err
	}
	if openedIdentity.Dev != stageIdentity.Dev || openedIdentity.Ino != stageIdentity.Ino || openedIdentity.Uid != uint32(os.Getuid()) || openedIdentity.Mode&0777 != 0700 || diagnosticAlias(staging) {
		return errPrivate
	}
	if err = privatizeDiagnosticExport(staging); err != nil {
		return err
	}
	var file *os.File
	var archiveIdentity, publishedIdentity unix.Stat_t
	published := false
	defer func() {
		fault := recover()
		if fault != nil {
			result = errPrivate
		}
		if result != nil && published && sameExportIdentity(parent, leaf, &publishedIdentity) {
			_ = unix.Unlinkat(int(parent.Fd()), leaf, 0)
			_ = parent.Sync()
		}
		if file != nil {
			if sameExportIdentity(staging, "archive", &archiveIdentity) {
				_ = unix.Unlinkat(int(staging.Fd()), "archive", 0)
			}
			file.Close()
		}
		if fault != nil {
			panic(fault)
		}
	}()
	fd, err = unix.Openat(int(staging.Fd()), "archive", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file = os.NewFile(uintptr(fd), "diagnostic-export")
	if err = unix.Fstat(fd, &archiveIdentity); err != nil {
		return err
	}
	if err = privatizeDiagnosticExport(file); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = write(file); err != nil {
		return err
	}
	if err = syncFile(file); err != nil {
		return err
	}
	if err = validateDiagnosticFile(file); err != nil {
		return err
	}
	if !sameExportIdentity(staging, "archive", &archiveIdentity) {
		return errPrivate
	}
	// A moved/replaced selected parent cannot silently change the chosen target.
	current, err := openExportParent(path, protected)
	if err != nil {
		return err
	}
	before, _ := parent.Stat()
	now, _ := current.Stat()
	current.Close()
	if before == nil || now == nil || !os.SameFile(before, now) {
		return errPrivate
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !sameExportIdentity(parent, temp, &stageIdentity) {
		return errPrivate
	}
	publishedIdentity = archiveIdentity
	if err = unix.Linkat(int(staging.Fd()), "archive", int(parent.Fd()), leaf, 0); err != nil {
		return err
	}
	published = true
	var actual unix.Stat_t
	if err = unix.Fstatat(int(parent.Fd()), leaf, &actual, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if archiveIdentity.Dev != actual.Dev || archiveIdentity.Ino != actual.Ino || actual.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return errPrivate
	}
	if err = syncFile(parent); err != nil {
		return err
	}
	if !sameExportIdentity(staging, "archive", &archiveIdentity) {
		return errPrivate
	}
	if err = unix.Unlinkat(int(staging.Fd()), "archive", 0); err != nil {
		return err
	}
	if err = syncFile(staging); err != nil {
		return err
	}
	if !sameExportIdentity(parent, temp, &stageIdentity) {
		return errPrivate
	}
	if err = unix.Unlinkat(int(parent.Fd()), temp, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	if err = syncFile(parent); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	file = nil
	return nil
}
