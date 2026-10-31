package diagnostics

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"time"

	v1 "personaldesk/internal/contracts/v1"
)

// SnapshotSource is implemented only by the lock-owned fixed log inventory.
// Visit runs on the capture writer, so rotation and shutdown cannot interleave.
// Each reader is bounded by the source; unavailable files are counted separately.
type SnapshotSource interface {
	Visit(func(io.Reader)) (unavailable int, incomplete bool)
}
type ArchiveSnapshot struct {
	Logs     []byte
	Coverage v1.Coverage
	Shared   string
}

const maxCount int64 = 9007199254740991

func boundedCount(a, n int64) int64 {
	if n > maxCount-a {
		return maxCount
	}
	return a + n
}

// Collect serializes a snapshot after previously queued capture deliveries. The
// reserved queue slot keeps capture nonblocking. Snapshot() remains memory-only.
func (b *Buffer) Collect(ctx context.Context) (ArchiveSnapshot, error) {
	b.snapshotMu.Lock()
	defer b.snapshotMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ArchiveSnapshot{}, err
	}
	b.sinkMu.Lock()
	b.mu.Lock()
	entries := append([]*retainedEvent(nil), b.events...)
	b.mu.Unlock()
	done := make(chan ArchiveSnapshot, 1)
	collect := func(sink Sink) {
		snap := ArchiveSnapshot{Shared: "unavailable", Coverage: v1.Coverage{Complete: true}}
		// Reserve ample space for the bounded manifest and empty template.
		const logBudget = v1.MaxArchiveBytes - v1.MaxDiagnosticRecordBytes
		var logs bytes.Buffer
		add := func(raw []byte) {
			event, err := v1.Decode[v1.DiagnosticEvent]("diagnostic-event", bytes.NewReader(raw), v1.MaxDiagnosticRecordBytes)
			if err != nil {
				snap.Coverage.OmittedRecords = boundedCount(snap.Coverage.OmittedRecords, 1)
				snap.Coverage.Complete = false
				return
			}
			// Decode + reserialize, never copy original/user-editable log bytes.
			line, err := json.Marshal(event)
			if err != nil || len(line)+1 > v1.MaxDiagnosticRecordBytes {
				snap.Coverage.OmittedRecords = boundedCount(snap.Coverage.OmittedRecords, 1)
				snap.Coverage.Complete = false
				return
			}
			if logs.Len()+len(line)+1 > logBudget {
				snap.Coverage.TruncatedRecords = boundedCount(snap.Coverage.TruncatedRecords, 1)
				snap.Coverage.Complete = false
				return
			}
			logs.Write(line)
			logs.WriteByte('\n')
			snap.Coverage.AcceptedRecords = boundedCount(snap.Coverage.AcceptedRecords, 1)
		}
		visited := 0
		visit := func(r io.Reader) {
			visited++
			reader := bufio.NewReaderSize(r, v1.MaxDiagnosticRecordBytes)
			for {
				line, err := reader.ReadSlice('\n')
				if errors.Is(err, bufio.ErrBufferFull) {
					// Discard an oversized record incrementally, with bounded memory.
					for errors.Is(err, bufio.ErrBufferFull) {
						_, err = reader.ReadSlice('\n')
					}
					snap.Coverage.OmittedRecords = boundedCount(snap.Coverage.OmittedRecords, 1)
					snap.Coverage.Complete = false
					if err != nil && !errors.Is(err, io.EOF) {
						snap.Coverage.Complete = false
					}
					if err != nil {
						return
					}
					continue
				}
				if len(line) > 0 {
					if line[len(line)-1] != '\n' {
						snap.Coverage.TruncatedRecords = boundedCount(snap.Coverage.TruncatedRecords, 1)
						snap.Coverage.Complete = false
					} else {
						add(line)
					}
				}
				if err != nil {
					if !errors.Is(err, io.EOF) {
						snap.Coverage.Complete = false
					}
					return
				}
			}
		}
		if source, ok := sink.(SnapshotSource); ok {
			snap.Shared = "available"
			if err := isolate(func() error {
				var incomplete bool
				snap.Coverage.UnavailableFiles, incomplete = source.Visit(visit)
				if incomplete {
					snap.Coverage.Complete = false
				}
				return nil
			}); err != nil {
				snap.Shared = "unavailable"
				snap.Coverage.UnavailableFiles = v1.RetainedLogBackups + 1
				snap.Coverage.Complete = false
			}
		} else {
			snap.Coverage.UnavailableFiles = v1.RetainedLogBackups + 1
			snap.Coverage.Complete = false
		}
		if visited == 0 && sink != nil {
			snap.Shared = "unavailable"
			snap.Coverage.Complete = false
		}
		b.mu.Lock()
		if b.unknownLoss {
			snap.Coverage.Complete = false
		}
		snap.Coverage.OmittedRecords = boundedCount(snap.Coverage.OmittedRecords, b.lost)
		private := make([]Event, 0, len(entries))
		for _, entry := range entries {
			if visited > 0 && entry.delivery == deliveryPersisted {
				continue
			}
			if visited > 0 && entry.delivery == deliveryAmbiguous {
				snap.Coverage.OmittedRecords = boundedCount(snap.Coverage.OmittedRecords, 1)
				continue
			}
			private = append(private, entry.event)
		}
		b.mu.Unlock()
		for _, event := range private {
			raw, _ := json.Marshal(event)
			add(raw)
		}
		if snap.Coverage.OmittedRecords > 0 || snap.Coverage.UnavailableFiles > 0 || snap.Coverage.TruncatedRecords > 0 {
			snap.Coverage.Complete = false
		}
		snap.Logs = logs.Bytes()
		done <- snap
	}
	if b.writer != nil && !b.closed {
		b.writer.queue <- writerJob{snapshot: collect}
		b.sinkMu.Unlock()
	} else {
		b.sinkMu.Unlock()
		collect(nil)
	}
	out := <-done
	if err := ctx.Err(); err != nil {
		return ArchiveSnapshot{}, err
	}
	return out, nil
}

// Manifest contains only typed runtime metadata and startup-cached schema facts.
// Unknown values remain null; this method never inspects the host or database.
func (b *Buffer) Manifest(status string, version *int64, snap ArchiveSnapshot) v1.Manifest {
	osName, arch, goVersion, wails := runtime.GOOS, runtime.GOARCH, runtime.Version(), "3.0.0"
	session := b.session
	m := v1.Manifest{SchemaVersion: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), AppVersion: "0.1.0", BuildVersion: buildVersion, OS: &osName, Arch: &arch, Runtime: v1.RuntimeVersions{Go: &goVersion, Wails: &wails}, SchemaStatus: status, SchemaVersionApplied: version, SessionID: &session, SharedDiagnostics: snap.Shared, Coverage: snap.Coverage}
	// Runtime values are optional. A future toolchain outside the frozen catalog
	// must not make an otherwise useful export fail.
	raw, _ := json.Marshal(m)
	if _, err := v1.Validate("manifest", bytes.NewReader(raw), v1.MaxDiagnosticRecordBytes); err != nil {
		m.Runtime = v1.RuntimeVersions{}
		if arch != "arm64" && arch != "amd64" {
			m.Arch = nil
		}
		if osName != "darwin" && osName != "linux" && osName != "windows" {
			m.OS = nil
		}
	}
	return m
}

func WriteArchive(ctx context.Context, w io.Writer, m v1.Manifest, snap ArchiveSnapshot) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err = v1.Validate("manifest", bytes.NewReader(raw), v1.MaxDiagnosticRecordBytes); err != nil {
		return err
	}
	if len(raw)+len(snap.Logs) > v1.MaxArchiveBytes {
		return errSink
	}
	z := zip.NewWriter(w)
	for _, entry := range []struct {
		name string
		data []byte
	}{{v1.ManifestEntry, raw}, {v1.DiagnosticLogEntry, snap.Logs}, {v1.ReproductionEntry, nil}} {
		if err = ctx.Err(); err != nil {
			return err
		}
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		h.SetMode(0600)
		out, e := z.CreateHeader(h)
		if e != nil {
			return e
		}
		if _, e = out.Write(entry.data); e != nil {
			return e
		}
	}
	return z.Close()
}
