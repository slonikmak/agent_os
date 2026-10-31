// Package diagnostics owns one bounded, content-free diagnostic pipeline.
// Capture is deliberately nonthrowing: an unavailable sink cannot change a
// committed business result. The ring remains useful before ownership and after
// close, when this process must not touch shared files.
package diagnostics

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	v1 "personaldesk/internal/contracts/v1"
)

type Event = v1.DiagnosticEvent

const capacity = 128

// Sink is owned by bootstrap under the workspace lock. Append receives a single
// complete validated JSON line; it must not call back into diagnostics.
type Sink interface {
	Append([]byte) error
	Close() error
}
type Status struct {
	SessionID               string `json:"sessionId"`
	Degraded                bool   `json:"degraded"`
	SharedAvailable         bool   `json:"sharedAvailable"`
	RestartRequired         bool   `json:"restartRequired"`
	InterruptedPriorSession bool   `json:"interruptedPriorSession"`
}
type Buffer struct {
	mu            sync.Mutex
	events        []*retainedEvent
	lost          int64
	unknownLoss   bool
	session       string
	degraded      bool
	restart       bool
	interrupted   bool
	snapshotMu    sync.Mutex
	sinkMu        sync.Mutex
	writer        *sinkWriter
	closed        bool
	closeDone     chan struct{}
	frontendTimes []time.Time
	repeats       map[string]repeat
	now           func() time.Time
}
type repeat struct {
	at time.Time
	id string
}

func New() *Buffer {
	return &Buffer{session: ID(), closeDone: make(chan struct{}), repeats: make(map[string]repeat), now: time.Now}
}

var fallbackSequence atomic.Uint64

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	// Correlation remains opaque and process-unique if the OS random source fails.
	// This fallback is never used for authentication material (bootstrap generates
	// credentials separately using crypto/rand).
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", time.Now().UnixNano(), fallbackSequence.Add(1))))
	return hex.EncodeToString(sum[:16])
}
func (b *Buffer) Record(code, operation, requestID, errorID string) {
	component := "bootstrap"
	if strings.HasSuffix(operation, ".read") || strings.HasPrefix(operation, "inbox.") {
		component = "httpapi"
	}
	if code == "framework.event" {
		component = "framework"
	}
	if code == "db.init" || code == "migration.step" {
		component = "database"
	}
	b.RecordComponent(component, code, operation, requestID, errorID)
}
func (b *Buffer) RecordComponent(component, code, operation, requestID, errorID string) {
	level := "info"
	if code == "framework.event" {
		level = "warn"
	}
	if code == "query.fault" || strings.HasPrefix(code, "ui.") {
		level = "error"
	}
	b.Capture(Event{Level: level, Code: code, Component: component, Operation: operation, RequestID: requestID, ErrorID: errorID})
}

// Capture validates before retaining any bytes, including framework-derived data.
func (b *Buffer) Capture(event Event) (accepted bool) {
	if b == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			b.markDegraded()
			accepted = false
		}
	}()
	event.SchemaVersion = 1
	event.At = b.now().UTC().Format(time.RFC3339Nano)
	event.SessionID = b.session
	event.AppVersion = "0.1.0"
	event.BuildVersion = buildVersion
	data, err := json.Marshal(event)
	if err != nil || len(data)+1 > v1.MaxDiagnosticRecordBytes {
		return false
	}
	if _, err = v1.Validate("diagnostic-event", bytes.NewReader(data), v1.MaxDiagnosticRecordBytes); err != nil {
		return false
	}
	event.Frames = append([]v1.Frame(nil), event.Frames...)
	if event.DurationMs != nil {
		duration := *event.DurationMs
		event.DurationMs = &duration
	}
	b.sinkMu.Lock()
	defer b.sinkMu.Unlock()
	b.mu.Lock()
	entry := &retainedEvent{event: event}
	if len(b.events) == capacity {
		old := b.events[0]
		old.evicted = true
		if old.delivery == deliveryPrivate || old.delivery == deliveryAmbiguous {
			b.lost = boundedCount(b.lost, 1)
		}
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = entry
	} else {
		b.events = append(b.events, entry)
	}
	// One owned writer drains a fixed queue. A slow/failed filesystem cannot
	// block a business result; overflow remains in the bounded private ring.
	if b.writer != nil && !b.closed {
		if len(b.writer.queue) < capacity {
			entry.delivery = deliveryQueued
			b.writer.queue <- writerJob{line: append(data, '\n'), entry: entry}
		} else {
			b.degraded = true
		}
	}
	b.mu.Unlock()
	return true
}
func isolate(fn func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = errSink
		}
	}()
	return fn()
}

var errSink = fmt.Errorf("diagnostic sink unavailable")

func (b *Buffer) markDegraded() { b.mu.Lock(); b.degraded = true; b.unknownLoss = true; b.mu.Unlock() }

type deliveryState uint8

const (
	deliveryPrivate deliveryState = iota
	deliveryQueued
	deliveryPersisted
	// A failed append may have written bytes before failing (e.g. at fsync).
	// Collect counts it as omitted rather than duplicating it from private memory.
	deliveryAmbiguous
)

// Each retainedEvent is a private capture identity, unrelated to correlation IDs.
type retainedEvent struct {
	event    Event
	delivery deliveryState
	evicted  bool
}
type writerJob struct {
	line     []byte
	entry    *retainedEvent
	snapshot func(Sink)
}
type sinkWriter struct {
	sink  Sink
	queue chan writerJob
	done  chan struct{}
}

func (b *Buffer) Attach(sink Sink, interrupted bool) {
	b.sinkMu.Lock()
	defer b.sinkMu.Unlock()
	if b.closed {
		return
	}
	if sink == nil {
		b.markDegraded()
		return
	}
	if b.writer != nil {
		b.mu.Lock()
		b.unknownLoss = true
		b.mu.Unlock()
		close(b.writer.queue)
		<-b.writer.done
	}
	writer := &sinkWriter{sink: sink, queue: make(chan writerJob, capacity+1), done: make(chan struct{})}
	b.writer = writer
	b.mu.Lock()
	b.interrupted = interrupted
	b.mu.Unlock()
	go func() {
		defer close(writer.done)
		for job := range writer.queue {
			if job.snapshot != nil {
				job.snapshot(writer.sink)
				continue
			}
			err := isolate(func() error { return writer.sink.Append(job.line) })
			b.mu.Lock()
			job.entry.delivery = deliveryPersisted
			if err != nil {
				job.entry.delivery = deliveryAmbiguous
				b.degraded = true
				if job.entry.evicted {
					b.lost = boundedCount(b.lost, 1)
				}
			}
			b.mu.Unlock()
		}
		if isolate(writer.sink.Close) != nil {
			b.markDegraded()
		}
	}()
}

func (b *Buffer) Degrade()        { b.markDegraded() }
func (b *Buffer) RequireRestart() { b.mu.Lock(); b.restart = true; b.mu.Unlock() }
func (b *Buffer) Status() Status {
	b.sinkMu.Lock()
	defer b.sinkMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	return Status{b.session, b.degraded, b.writer != nil && !b.closed, b.restart, b.interrupted}
}
func (b *Buffer) Close() {
	b.sinkMu.Lock()
	if b.closed {
		done := b.closeDone
		b.sinkMu.Unlock()
		<-done
		return
	}
	b.closed = true
	writer := b.writer
	b.writer = nil
	if writer != nil {
		close(writer.queue)
	}
	b.sinkMu.Unlock() // late callbacks immediately use private memory only
	if writer != nil {
		<-writer.done
	}
	close(b.closeDone)
}

func (b *Buffer) Snapshot() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, len(b.events))
	for i, entry := range b.events {
		out[i] = entry.event
	}
	for i := range out {
		out[i].Frames = append([]v1.Frame(nil), out[i].Frames...)
		if out[i].DurationMs != nil {
			duration := *out[i].DurationMs
			out[i].DurationMs = &duration
		}
	}
	return out
}
func (b *Buffer) Operation(component, operation, requestID string, started time.Time) {
	duration := time.Since(started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	if duration > 86400000 {
		duration = 86400000
	}
	b.Capture(Event{Level: "info", Code: "api.operation", Component: component, Operation: operation, RequestID: requestID, DurationMs: &duration})
}
func (b *Buffer) Fault(component, operation, requestID, class string) string {
	if requestID == "" {
		requestID = ID()
	}
	id := ID()
	b.Capture(Event{Level: "error", Code: "query.fault", Component: component, Operation: operation, RequestID: requestID, ErrorID: id, ErrorClass: class, Frames: Frames()})
	return id
}

// Frames maps known source files to the frozen catalog. Raw stack strings,
// function names, arguments and absolute filenames never enter an event.
func Frames() []v1.Frame {
	pcs := make([]uintptr, 24)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	out := []v1.Frame{}
	for {
		f, more := frames.Next()
		location := ""
		switch {
		case strings.HasSuffix(f.File, "/bootstrap/workspace.go"):
			location = "bootstrap.start"
		case strings.HasSuffix(f.File, "/queries/workspace.go"):
			location = "queries.workspace"
		case strings.HasSuffix(f.File, "/adapters/httpapi/handler.go"):
			location = "httpapi.serve"
		case strings.HasSuffix(f.File, "/adapters/wailsread/facade.go"):
			location = "wailsread.workspace"
		case strings.HasSuffix(f.File, "/sqlite/migrate.go"):
			location = "sqlite.open"
		case strings.HasSuffix(f.File, "/sqlite/workspace.go"):
			location = "sqlite.workspace"
		}
		if location != "" && f.Line > 0 && f.Line <= 1000000 {
			out = append(out, v1.Frame{Location: location, Line: f.Line})
			if len(out) == 16 {
				break
			}
		}
		if !more {
			break
		}
	}
	return out
}

type FrontendResult struct {
	Status    string  `json:"status"`
	RequestID string  `json:"requestId"`
	ErrorID   *string `json:"errorId"`
}

func (b *Buffer) ReportFrontend(raw string) (result FrontendResult) {
	result = FrontendResult{Status: "rejected", RequestID: ID()}
	defer func() {
		if recover() != nil {
			b.markDegraded()
			result.Status = "unavailable"
		}
	}()
	if len(raw) > v1.MaxFrontendReportBytes {
		return
	}
	report, err := v1.Decode[v1.FrontendFault]("frontend-fault", strings.NewReader(raw), v1.MaxFrontendReportBytes)
	if err != nil {
		return
	}
	now := b.now()
	key := report.Code + ":" + report.Component
	b.mu.Lock()
	if prior, ok := b.repeats[key]; ok && now.Sub(prior.at) < time.Duration(v1.FrontendRepeatWindowSeconds)*time.Second {
		b.mu.Unlock()
		result.Status = "suppressed"
		result.ErrorID = &prior.id
		return
	}
	first := 0
	for first < len(b.frontendTimes) && now.Sub(b.frontendTimes[first]) >= time.Minute {
		first++
	}
	b.frontendTimes = append(b.frontendTimes[:0], b.frontendTimes[first:]...)
	if len(b.frontendTimes) >= v1.FrontendReportsPerMinute {
		b.mu.Unlock()
		result.Status = "suppressed"
		return
	}
	id := ID()
	b.frontendTimes = append(b.frontendTimes, now)
	b.repeats[key] = repeat{now, id}
	b.mu.Unlock()
	if !b.Capture(Event{Level: "error", Code: report.Code, Component: "frontend", Operation: "frontend.report", RequestID: result.RequestID, ErrorID: id, ErrorClass: "runtime", Frames: report.Frames}) {
		result.Status = "unavailable"
		return
	}
	result.Status = "accepted"
	result.ErrorID = &id
	return
}

// Handler rejects every raw framework message and attribute.
type Handler struct{ Buffer *Buffer }

func (h Handler) Enabled(_ context.Context, level slog.Level) bool { return level >= slog.LevelWarn }
func (h Handler) Handle(_ context.Context, _ slog.Record) error {
	h.Buffer.Record("framework.event", "framework", "", "")
	return nil
}
func (h Handler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h Handler) WithGroup(_ string) slog.Handler      { return h }
