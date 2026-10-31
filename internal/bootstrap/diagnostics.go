package bootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"personaldesk/internal/adapters/diagnostics"
	v1 "personaldesk/internal/contracts/v1"
)

// This fixed inventory is separate from SQLite and materials. The anchored
// directory remains pinned for the whole owned session; no recursive scan or
// caller-selected filename is used. Every operation ends before lock release.
type diagnosticFiles struct {
	dir        *diagnosticDirectory
	active     *os.File
	size       int64
	session    string
	limit      int64
	incomplete bool
	present    [v1.RetainedLogBackups + 1]bool
}
type sessionMarker struct {
	SessionID string `json:"sessionId"`
	Clean     bool   `json:"clean"`
}

func logName(n int) string {
	if n == 0 {
		return "diagnostics.jsonl"
	}
	return fmt.Sprintf("diagnostics.%d.jsonl", n)
}
func openDiagnostics(root, session string) (_ *diagnosticFiles, interrupted bool, result error) {
	dir, err := openDiagnosticDirectory(root)
	if err != nil {
		return nil, false, err
	}
	d := &diagnosticFiles{dir: dir, session: session, limit: v1.MaxActiveLogBytes}
	defer func() {
		if result != nil {
			_ = d.CloseFiles()
		}
	}()
	// Reject aliases and unsafe files across the entire retained inventory before
	// cleanup can rename/remove one. No other owner's file is read before locking.
	for n := 0; n <= v1.RetainedLogBackups; n++ {
		f, err := openPrivateIn(dir, logName(n), os.O_RDONLY)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		d.present[n] = true
		info, err := f.Stat()
		f.Close()
		if err != nil {
			return nil, false, err
		}
		if info.Size() > v1.MaxActiveLogBytes {
			d.incomplete = true
			if err = dir.Remove(logName(n)); err != nil {
				return nil, false, err
			}
		}
	}
	marker, err := openPrivateIn(dir, "session.json", os.O_RDONLY)
	if err == nil {
		data, readErr := io.ReadAll(io.LimitReader(marker, 257))
		marker.Close()
		var prior sessionMarker
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&prior)
		canonical, _ := json.Marshal(prior)
		interrupted = readErr != nil || len(data) > 256 || decodeErr != nil || decoder.Decode(&struct{}{}) != io.EOF || !bytes.Equal(data, canonical) || !validSessionID(prior.SessionID) || !prior.Clean
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	d.present[0] = true
	d.active, err = openPrivateIn(dir, logName(0), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, false, err
	}
	info, err := d.active.Stat()
	if err != nil {
		return nil, false, err
	}
	// A killed writer can leave a partial final record. Retain only complete,
	// schema-valid lines; untrusted edits never become diagnostic history.
	data, err := io.ReadAll(io.LimitReader(d.active, v1.MaxActiveLogBytes+1))
	if err != nil {
		return nil, false, err
	}
	good := []byte{}
	for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if line[len(line)-1] != '\n' || len(line) > v1.MaxDiagnosticRecordBytes {
			continue
		}
		if _, err := v1.Validate("diagnostic-event", bytes.NewReader(line), v1.MaxDiagnosticRecordBytes); err == nil {
			good = append(good, line...)
		}
	}
	if int64(len(good)) != info.Size() {
		d.incomplete = true
		if err = d.active.Truncate(0); err != nil {
			return nil, false, err
		}
		if _, err = d.active.WriteAt(good, 0); err != nil {
			return nil, false, err
		}
	}
	d.size = int64(len(good))
	if _, err = d.active.Seek(d.size, io.SeekStart); err != nil {
		return nil, false, err
	}
	if err = d.marker(false); err != nil {
		return nil, false, err
	}
	return d, interrupted, nil
}
func validSessionID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (d *diagnosticFiles) marker(clean bool) error {
	f, err := openPrivateIn(d.dir, "session.json", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	data, _ := json.Marshal(sessionMarker{d.session, clean})
	if err = f.Truncate(0); err != nil {
		return err
	}
	if _, err = f.WriteAt(data, 0); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return d.dir.Sync()
}
func (d *diagnosticFiles) Append(line []byte) error {
	if err := validateDiagnosticFile(d.active); err != nil {
		return err
	}
	if len(line) > v1.MaxDiagnosticRecordBytes || len(line) == 0 || line[len(line)-1] != '\n' {
		return errPrivate
	}
	if d.size+int64(len(line)) > d.limit {
		if err := d.rotate(); err != nil {
			return err
		}
	}
	n, err := d.active.Write(line)
	d.size += int64(n)
	if err != nil || n != len(line) { // Roll back a partial append, never retain a torn record.
		d.size -= int64(n)
		_ = d.active.Truncate(d.size)
		_, _ = d.active.Seek(d.size, io.SeekStart)
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	return d.active.Sync()
}
func (d *diagnosticFiles) rotate() error {
	// Revalidate each name before maintenance. Renaming an alias never follows
	// its target, and every subsequently opened file is checked again.
	for n := 0; n <= v1.RetainedLogBackups; n++ {
		f, err := openPrivateIn(d.dir, logName(n), os.O_RDONLY)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		f.Close()
	}
	if err := d.active.Sync(); err != nil {
		return err
	}
	if err := d.active.Close(); err != nil {
		return err
	}
	d.active = nil
	if err := d.dir.Remove(logName(v1.RetainedLogBackups)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for n := v1.RetainedLogBackups - 1; n >= 0; n-- {
		if err := d.dir.Rename(logName(n), logName(n+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	f, err := openPrivateIn(d.dir, logName(0), os.O_CREATE|os.O_EXCL|os.O_RDWR)
	if err != nil {
		return err
	}
	d.active = f
	for n := v1.RetainedLogBackups; n > 0; n-- {
		d.present[n] = d.present[n-1]
	}
	d.present[0] = true
	d.size = 0
	return d.dir.Sync()
}
func (d *diagnosticFiles) CloseFiles() error {
	var err error
	if d.active != nil {
		err = errors.Join(d.active.Sync(), d.active.Close())
		d.active = nil
	}
	if d.dir != nil {
		err = errors.Join(err, d.dir.Close())
		d.dir = nil
	}
	return err
}
func (d *diagnosticFiles) Close() error {
	var err error
	if d.active != nil {
		err = d.active.Sync()
	}
	err = errors.Join(err, d.marker(true))
	return errors.Join(err, d.CloseFiles())
}
func attachDiagnostics(root string, diag *diagnostics.Buffer) {
	files, interrupted, err := openDiagnostics(root, diag.Status().SessionID)
	if err != nil {
		diag.Degrade()
		return
	}
	diag.Attach(files, interrupted)
	if files.incomplete {
		diag.Degrade()
	}
	if interrupted {
		diag.Capture(diagnostics.Event{Level: "warn", Code: "framework.event", Component: "bootstrap", Operation: "startup", ErrorClass: "runtime"})
	}
}

// Visit uses the already retained directory handle and fixed inventory only.
// It runs on the single capture writer, after earlier appends, before rotation.
func (d *diagnosticFiles) Visit(visit func(io.Reader)) (unavailable int, incomplete bool) {
	incomplete = d.incomplete
	for n := v1.RetainedLogBackups; n >= 0; n-- {
		var f *os.File
		var err error
		if n == 0 {
			f = d.active
			err = validateDiagnosticFile(f)
		} else {
			f, err = openPrivateIn(d.dir, logName(n), os.O_RDONLY)
		}
		if errors.Is(err, os.ErrNotExist) && !d.present[n] {
			continue
		}
		if err != nil {
			unavailable++
			continue
		}
		info, err := f.Stat()
		if err != nil {
			unavailable++
			if n != 0 {
				f.Close()
			}
			continue
		}
		if info.Size() > v1.MaxActiveLogBytes {
			incomplete = true
		}
		visit(io.NewSectionReader(f, 0, v1.MaxActiveLogBytes))
		if n != 0 {
			if err = f.Close(); err != nil {
				incomplete = true
			}
		}
	}
	return
}
