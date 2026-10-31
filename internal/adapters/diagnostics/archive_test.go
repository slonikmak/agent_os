package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	v1 "personaldesk/internal/contracts/v1"
	"strings"
	"sync"
	"testing"
	"time"
)

type snapshotSink struct {
	lines   bytes.Buffer
	raw     []byte
	blocked chan struct{}
	entered chan struct{}
	fail    bool
	once    sync.Once
}

func (s *snapshotSink) Append(line []byte) error {
	if s.blocked != nil {
		s.once.Do(func() { close(s.entered) })
		<-s.blocked
	}
	s.lines.Write(line)
	if s.fail {
		return errSink
	}
	return nil
}
func (s *snapshotSink) Close() error { return nil }
func (s *snapshotSink) Visit(visit func(io.Reader)) (int, bool) {
	visit(bytes.NewReader(s.raw))
	visit(bytes.NewReader(s.lines.Bytes()))
	return 0, false
}

func TestExportRevalidatesReserializesAndCountsHostileRecords(t *testing.T) {
	b := New()
	defer b.Close()
	b.Record("app.start", "startup", "", "")
	event := b.Snapshot()[0]
	raw, _ := json.Marshal(event)
	// JSON whitespace is not retained; fields with secret values are omitted.
	s := &snapshotSink{raw: append(append([]byte(" \t"), raw...), '\n')}
	s.raw = append(s.raw, []byte(`{"schemaVersion":99,"SECRET":"SENSITIVE /private"}`+"\n"+`{"schemaVersion":1,"at":"SENSITIVE"}`+"\n"+strings.Repeat("x", v1.MaxDiagnosticRecordBytes+1)+"\n"+`{"unfinished":"SECRET"}`)...)
	b.Attach(s, false)
	snap, err := b.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Coverage.Complete || snap.Coverage.AcceptedRecords != 2 || snap.Coverage.OmittedRecords != 3 || snap.Coverage.TruncatedRecords != 1 {
		t.Fatalf("coverage %+v", snap.Coverage)
	}
	if bytes.Contains(snap.Logs, []byte("SECRET")) || bytes.Contains(snap.Logs, []byte("SENSITIVE")) || bytes.Contains(snap.Logs, []byte(" \t")) {
		t.Fatal("untrusted bytes survived")
	}
	var archive bytes.Buffer
	if err = WriteArchive(context.Background(), &archive, b.Manifest("unknown", nil, snap), snap); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 3 {
		t.Fatal("unexpected entries")
	}
	names := v1.ArchiveEntries()
	total := uint64(0)
	for i, file := range z.File {
		if file.Name != names[i] || !file.Mode().IsRegular() || file.Mode().Perm() != 0600 {
			t.Fatal("unsafe ZIP entry")
		}
		total += file.UncompressedSize64
		r, _ := file.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		if i == 0 {
			if _, err = v1.Validate("manifest", bytes.NewReader(data), v1.MaxDiagnosticRecordBytes); err != nil {
				t.Fatal(err)
			}
		}
		if i == 2 && len(data) != 0 {
			t.Fatal("nonblank template")
		}
	}
	if total > v1.MaxArchiveBytes {
		t.Fatal("archive budget")
	}
}

func TestSnapshotDrainsEarlierCaptureUsesIdentityAndReportsLoss(t *testing.T) {
	b := New()
	defer b.Close()
	fixed := time.Now()
	b.now = func() time.Time { return fixed }
	s := &snapshotSink{blocked: make(chan struct{}), entered: make(chan struct{})}
	b.Attach(s, false)
	// Exact same event bytes and correlation IDs are distinct capture identities.
	for range 2 {
		b.Record("app.start", "startup", "", "")
	}
	<-s.entered
	finished := make(chan ArchiveSnapshot, 1)
	go func() { snap, _ := b.Collect(context.Background()); finished <- snap }()
	select {
	case <-finished:
		t.Fatal("snapshot passed prior pending append")
	case <-time.After(10 * time.Millisecond):
	}
	close(s.blocked)
	snap := <-finished
	if snap.Coverage.AcceptedRecords != 2 || !snap.Coverage.Complete {
		t.Fatalf("duplicate/lost record %+v", snap.Coverage)
	}
	s.fail = true
	b.Record("app.stop", "shutdown", "", "")
	snap, _ = b.Collect(context.Background())
	// Failed fsync may leave the shared line; never append it a second time.
	if snap.Coverage.AcceptedRecords != 3 || snap.Coverage.OmittedRecords != 1 || snap.Coverage.Complete {
		t.Fatalf("ambiguous delivery %+v", snap.Coverage)
	}
	private := New()
	defer private.Close()
	for range capacity + 7 {
		private.Record("app.start", "startup", "", "")
	}
	snap, _ = private.Collect(context.Background())
	if snap.Shared != "unavailable" || snap.Coverage.AcceptedRecords != capacity || snap.Coverage.OmittedRecords != 7 || snap.Coverage.Complete {
		t.Fatalf("ring loss %+v", snap.Coverage)
	}
}

func TestArchiveBudgetAndQueueOverflowRemainBounded(t *testing.T) {
	b := New()
	defer b.Close()
	b.Record("app.start", "startup", "", "")
	line, _ := json.Marshal(b.Snapshot()[0])
	line = append(line, '\n')
	s := &snapshotSink{raw: bytes.Repeat(line, (v1.MaxArchiveBytes/len(line))+4)}
	b.Attach(s, false)
	snap, _ := b.Collect(context.Background())
	if len(snap.Logs) > v1.MaxArchiveBytes-v1.MaxDiagnosticRecordBytes || snap.Coverage.TruncatedRecords == 0 || snap.Coverage.Complete {
		t.Fatalf("budget %+v", snap.Coverage)
	}
	blocked := &snapshotSink{blocked: make(chan struct{}), entered: make(chan struct{})}
	b.Attach(blocked, false)
	b.Record("app.start", "startup", "", "")
	<-blocked.entered
	for range 3 * capacity {
		b.Record("app.start", "startup", "", "")
	}
	done := make(chan ArchiveSnapshot, 1)
	go func() { s, _ := b.Collect(context.Background()); done <- s }()
	close(blocked.blocked)
	snap = <-done
	if snap.Coverage.Complete || snap.Coverage.OmittedRecords == 0 {
		t.Fatalf("hidden queue/ring loss %+v", snap.Coverage)
	}
}

func TestUnknownLossNeverClaimsCompleteCoverage(t *testing.T) {
	b := New()
	defer b.Close()
	b.Attach(&snapshotSink{}, false)
	b.Record("app.start", "startup", "", "")
	b.Degrade()
	snap, err := b.Collect(context.Background())
	if err != nil || snap.Coverage.Complete || snap.Coverage.AcceptedRecords != 1 {
		t.Fatalf("unknown loss %+v %v", snap.Coverage, err)
	}
}
