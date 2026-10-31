package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	v1 "personaldesk/internal/contracts/v1"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFallbackIsBoundedAndDropsUntrustedFields(t *testing.T) {
	buffer := New()
	logger := slog.New(Handler{buffer}).With("secret", "TOKEN SQL /private/home")
	for range 200 {
		logger.ErrorContext(context.Background(), "BUSINESS RAW PANIC", "request", "SENSITIVE")
	}
	buffer.Record("unknown SECRET", "workspace.read", "", "")
	buffer.Record("query.fault", "workspace.read", "untrusted", "")
	events := buffer.Snapshot()
	if len(events) != capacity {
		t.Fatalf("count=%d", len(events))
	}
	encoded, _ := json.Marshal(events)
	for _, marker := range []string{"TOKEN", "SQL", "/private", "BUSINESS", "SENSITIVE", "untrusted"} {
		if strings.Contains(string(encoded), marker) {
			t.Fatalf("leaked %s", marker)
		}
	}
	events[0].Code = "tamper"
	if buffer.Snapshot()[0].Code == "tamper" {
		t.Fatal("snapshot aliases buffer")
	}
}

func TestFallbackEventsUseVersionedContractAndRetainBootstrapCorrelation(t *testing.T) {
	buffer := New()
	requestID := ID()
	errorID := ID()
	buffer.Record("app.start", "startup", "", "")
	buffer.Record("db.init", "startup", "", "")
	buffer.Record("query.fault", "startup", requestID, errorID)
	buffer.RecordComponent("wailsread", "query.fault", "workspace.read", requestID, errorID)
	buffer.RecordComponent("PRIVATE /home", "query.fault", "workspace.read", requestID, errorID)
	events := buffer.Snapshot()
	if len(events) != 4 {
		t.Fatalf("event count %d", len(events))
	}
	for _, event := range events {
		data, _ := json.Marshal(event)
		if _, err := v1.Validate("diagnostic-event", bytes.NewReader(data), v1.MaxDiagnosticRecordBytes); err != nil {
			t.Fatalf("invalid fallback %s", data)
		}
	}
	if events[2].At == "" || events[2].Code != "query.fault" || events[2].Operation != "startup" || events[2].RequestID != requestID || events[2].ErrorID != errorID || events[3].Component != "wailsread" {
		t.Fatal("bootstrap/native technical facts lost")
	}
}

type faultSink struct {
	panicWrite, panicClose bool
	writes, closes         int
	err                    error
}

func (s *faultSink) Append(_ []byte) error {
	s.writes++
	if s.panicWrite {
		panic("SECRET raw panic /private")
	}
	return s.err
}
func (s *faultSink) Close() error {
	s.closes++
	if s.panicClose {
		panic("SECRET close")
	}
	return s.err
}
func TestSinkFailurePanicAndLateCaptureAreIsolated(t *testing.T) {
	for _, panics := range []bool{false, true} {
		b := New()
		sink := &faultSink{panicWrite: panics, panicClose: panics, err: errSink}
		b.Attach(sink, false)
		id := b.Fault("httpapi", "inbox.receive", ID(), "internal")
		b.Close()
		if !b.Status().Degraded || id == "" || len(b.Snapshot()) != 1 {
			t.Fatal("fault not retained/degraded")
		}
		b.Close()
		writes := sink.writes
		b.Record("app.stop", "shutdown", "", "")
		b.Close()
		if sink.writes != writes || sink.closes != 1 || b.Status().SharedAvailable {
			t.Fatal("shared writes after close")
		}
		raw, _ := json.Marshal(b.Snapshot())
		if strings.Contains(string(raw), "SECRET") {
			t.Fatal("sink error escaped")
		}
	}
}
func TestEventCatalogAndEveryBound(t *testing.T) {
	b := New()
	id := ID()
	max := int64(86400000)
	for _, operation := range []string{"startup", "shutdown", "workspace.read", "discovery.read", "task.read", "inbox.read", "material.read", "inbox.receive", "inbox.resolve.createTask", "migration", "framework", "frontend.report", "diagnostics.export"} {
		if !b.Capture(Event{Level: "info", Code: "api.operation", Component: "httpapi", Operation: operation, RequestID: id, DurationMs: &max}) {
			t.Fatal(operation)
		}
	}
	for _, bad := range []Event{
		{Level: "error", Code: "query.fault", Component: "httpapi", Operation: "material.upload"},
		{Level: "error", Code: "query.fault", Component: "httpapi", Operation: "task.read", ErrorID: strings.Repeat("A", 32)},
		{Level: "error", Code: "query.fault", Component: "httpapi", Operation: "task.read", Frames: []v1.Frame{{Location: "/private/SECRET", Line: 1}}},
		{Level: "error", Code: "query.fault", Component: "httpapi", Operation: "task.read", Frames: make([]v1.Frame, 17)},
		{Level: "error", Code: strings.Repeat("S", v1.MaxDiagnosticRecordBytes), Component: "httpapi", Operation: "task.read"},
	} {
		if b.Capture(bad) {
			t.Fatal("accepted unallowlisted input")
		}
	}
	snapshot := b.Snapshot()
	for _, e := range snapshot {
		if e.SessionID == "" || e.At == "" || e.AppVersion != "0.1.0" {
			t.Fatal("missing session/version")
		}
	}
}
func TestFrontendValidationRateRepeatAndFaultCorrelation(t *testing.T) {
	b := New()
	now := time.Now()
	b.now = func() time.Time { return now }
	raw := `{"schemaVersion":1,"code":"ui.render_fault","component":"root","frames":[]}`
	accepted := b.ReportFrontend(raw)
	again := b.ReportFrontend(raw)
	if accepted.Status != "accepted" || accepted.ErrorID == nil || again.Status != "suppressed" || *again.ErrorID != *accepted.ErrorID {
		t.Fatal("repeat correlation")
	}
	for _, bad := range []string{strings.Repeat("S", 4097), strings.Replace(raw, `"frames":[]`, `"secret":"BUSINESS","frames":[]`, 1), strings.Replace(raw, `"frames":[]`, `"frames":[{"location":"/private","line":1}]`, 1), raw + `{}`} {
		if b.ReportFrontend(bad).Status != "rejected" {
			t.Fatal("invalid frontend report")
		}
	}
	// The minute limiter is independent of repetition state and bounded at 20.
	b.mu.Lock()
	b.repeats = map[string]repeat{}
	b.frontendTimes = make([]time.Time, v1.FrontendReportsPerMinute)
	for i := range b.frontendTimes {
		b.frontendTimes[i] = now
	}
	b.mu.Unlock()
	if b.ReportFrontend(raw).ErrorID != nil {
		t.Fatal("rate limited input captured")
	}
	now = now.Add(time.Minute)
	next := b.ReportFrontend(raw)
	if next.Status != "accepted" || *next.ErrorID == *accepted.ErrorID {
		t.Fatal("window did not expire")
	}
	e := b.Snapshot()[0]
	if e.ErrorID != *accepted.ErrorID || e.RequestID != accepted.RequestID || e.SessionID != b.Status().SessionID {
		t.Fatal("frontend ID not locatable")
	}
}

type blockedSink struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedSink) Append([]byte) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return nil
}
func (s *blockedSink) Close() error { return nil }
func TestSlowSinkHasBoundedQueueAndCloseDisablesLateSharedCallbacks(t *testing.T) {
	b := New()
	sink := &blockedSink{entered: make(chan struct{}), release: make(chan struct{})}
	b.Attach(sink, false)
	b.Record("app.start", "startup", "", "")
	<-sink.entered
	for range 300 {
		b.Record("api.operation", "workspace.read", ID(), "")
	}
	if !b.Status().Degraded || len(b.Snapshot()) != capacity {
		t.Fatal("queue overflow not bounded/private")
	}
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	deadline := time.After(time.Second)
	for b.Status().SharedAvailable {
		select {
		case <-deadline:
			t.Fatal("close did not disable shared callbacks")
		default:
			runtime.Gosched()
		}
	}
	captured := make(chan struct{})
	go func() { b.Record("app.stop", "shutdown", "", ""); close(captured) }()
	select {
	case <-captured:
	case <-time.After(time.Second):
		t.Fatal("late callback waited for shared I/O")
	}
	close(sink.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writer did not drain")
	}
}

func TestRetainedEventsDoNotAliasCaptureOrSnapshotPointers(t *testing.T) {
	b := New()
	duration := int64(5)
	event := Event{Level: "info", Code: "api.operation", Component: "httpapi", Operation: "workspace.read", DurationMs: &duration}
	if !b.Capture(event) {
		t.Fatal("event rejected")
	}
	duration = -1
	first := b.Snapshot()
	if *first[0].DurationMs != 5 {
		t.Fatal("capture pointer alias")
	}
	*first[0].DurationMs = -1
	if *b.Snapshot()[0].DurationMs != 5 {
		t.Fatal("snapshot pointer alias")
	}
}
