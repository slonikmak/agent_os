//go:build darwin || linux

// Package materials confines immutable content to an owned directory handle.
package materials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
	app "personaldesk/internal/application/materials"
)

var errUnsafe = errors.New("material content unavailable")

type Content struct {
	mu    sync.RWMutex
	dir   *os.File
	fault func(string) error
}

// Open walks from the filesystem root using no-follow directory handles. Every
// subsequent operation is relative to the retained materials handle, so renaming
// or replacing a pathname cannot redirect reads into workspace service files.
func Open(root string) (*Content, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errUnsafe
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errUnsafe
	}
	current := os.NewFile(uintptr(fd), "anchor")
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		current.Close()
		if e != nil {
			return nil, errUnsafe
		}
		current = os.NewFile(uintptr(next), "anchor")
		if alias(current) {
			current.Close()
			return nil, errUnsafe
		}
	}
	defer current.Close()
	if err = unix.Mkdirat(int(current.Fd()), "materials", 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, errUnsafe
	}
	fd, err = unix.Openat(int(current.Fd()), "materials", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errUnsafe
	}
	dir := os.NewFile(uintptr(fd), "materials")
	info, err := dir.Stat()
	if err != nil || !private(info, true) || alias(dir) {
		dir.Close()
		return nil, errUnsafe
	}
	// Make a newly created directory durable before any receipt can reference it.
	if err = dir.Sync(); err == nil {
		err = current.Sync()
	}
	if err != nil {
		dir.Close()
		return nil, errUnsafe
	}
	return &Content{dir: dir}, nil
}
func private(info os.FileInfo, directory bool) bool {
	if info == nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600 && stat.Nlink == 1
}
func validDigest(s string) bool { return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == "" }
func (c *Content) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dir == nil {
		return nil
	}
	err := c.dir.Close()
	c.dir = nil
	return err
}
func (c *Content) opened(name string) (*os.File, error) {
	fd, err := unix.Openat(int(c.dir.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "material")
	info, err := f.Stat()
	if err != nil || !private(info, false) || alias(f) {
		f.Close()
		return nil, errUnsafe
	}
	return f, nil
}
func (c *Content) read(ctx context.Context, digest string) ([]byte, string, error) {
	if !validDigest(digest) {
		return nil, "unsupported", nil
	}
	f, err := c.opened(digest)
	if errors.Is(err, unix.ENOENT) {
		return nil, "missing", nil
	}
	if err != nil {
		return nil, "unsupported", nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, "unsupported", nil
	}
	if info.Size() > app.MaxBytes {
		return nil, "too_large", nil
	}
	data := make([]byte, 0, min(info.Size(), app.MaxBytes))
	buffer := make([]byte, 32768)
	for len(data) <= app.MaxBytes {
		if err = ctx.Err(); err != nil {
			return nil, "", err
		}
		n, e := f.Read(buffer[:min(len(buffer), app.MaxBytes+1-len(data))])
		data = append(data, buffer[:n]...)
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, "unsupported", nil
		}
	}
	if len(data) > app.MaxBytes {
		return nil, "too_large", nil
	}
	after, err := f.Stat()
	if err != nil || !private(after, false) || alias(f) || after.Size() != int64(len(data)) {
		return nil, "unsupported", nil
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, "unsupported", nil
	}
	return data, "", nil
}
func (c *Content) Read(ctx context.Context, digest string) ([]byte, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	if c.dir == nil {
		return nil, "", errUnsafe
	}
	return c.read(ctx, digest)
}
func (c *Content) seam(stage string) error {
	if c.fault != nil {
		return c.fault(stage)
	}
	return nil
}
func (c *Content) Put(ctx context.Context, digest string, data []byte) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.dir == nil || !validDigest(digest) || len(data) > app.MaxBytes {
		return errUnsafe
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return errUnsafe
	}
	existing, reason, err := c.read(ctx, digest)
	if err != nil {
		return err
	}
	if reason == "" {
		if !strings.EqualFold(hex.EncodeToString(sum[:]), digest) || len(existing) != len(data) {
			return errUnsafe
		}
		return c.dir.Sync()
	}
	if reason != "missing" {
		return errUnsafe
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return errUnsafe
	}
	stage := "stage-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(int(c.dir.Fd()), stage, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return errUnsafe
	}
	f := os.NewFile(uintptr(fd), "staging")
	defer func() { f.Close(); _ = unix.Unlinkat(int(c.dir.Fd()), stage, 0) }()
	if err = c.seam("write"); err != nil {
		return err
	}
	for len(data) > 0 {
		if err = ctx.Err(); err != nil {
			return err
		}
		var n int
		n, err = f.Write(data[:min(len(data), 32768)])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	if err = c.seam("sync"); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = c.seam("publish"); err != nil {
		return err
	}
	// linkat is atomic and never replaces an existing hash. Remove the staging
	// name before verification so the canonical blob has exactly one hard link.
	err = unix.Linkat(int(c.dir.Fd()), stage, int(c.dir.Fd()), digest, 0)
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return errUnsafe
	}
	if err = unix.Unlinkat(int(c.dir.Fd()), stage, 0); err != nil {
		return errUnsafe
	}
	if _, reason, err = c.read(ctx, digest); err != nil || reason != "" {
		return errUnsafe
	}
	if err = c.seam("directory_sync"); err != nil {
		return err
	}
	if err = c.dir.Sync(); err != nil {
		return err
	}
	return ctx.Err()
}
