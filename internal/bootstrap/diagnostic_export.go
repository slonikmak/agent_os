package bootstrap

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"personaldesk/internal/adapters/diagnostics"
	v1 "personaldesk/internal/contracts/v1"
)

// DiagnosticMaintenance owns native-picker admission and the separate export
// lifetime. Domain cancellation/Halt never cancels maintenance. Close rejects
// late picker results and drains accepted file work before ownership is released.
type DiagnosticMaintenance struct {
	mu            sync.Mutex
	busy, closed  bool
	ctx           context.Context
	cancel        context.CancelFunc
	jobs          sync.WaitGroup
	diag          *diagnostics.Buffer
	pick          func() (string, error)
	protected     []protectedDestination
	schemaStatus  string
	schemaVersion *int64
}
type protectedDestination struct {
	path string
	info os.FileInfo
}

func NewDiagnosticMaintenance(diag *diagnostics.Buffer, configured string, workspace *Workspace, pick func() (string, error)) *DiagnosticMaintenance {
	ctx, cancel := context.WithCancel(context.Background())
	m := &DiagnosticMaintenance{ctx: ctx, cancel: cancel, diag: diag, pick: pick, schemaStatus: "unknown"}
	paths := []string{}
	if root, err := ResolveRoot(configured); err == nil {
		paths = append(paths, root)
	}
	if root, err := defaultRoot(); err == nil {
		paths = append(paths, root)
	}
	if executable, err := os.Executable(); err == nil {
		application := filepath.Dir(executable)
		for p := application; p != filepath.Dir(p); p = filepath.Dir(p) {
			if strings.HasSuffix(strings.ToLower(p), ".app") {
				application = p
				break
			}
		}
		paths = append(paths, application)
	}
	for _, path := range paths {
		info, _ := os.Stat(path)
		m.protected = append(m.protected, protectedDestination{filepath.Clean(path), info})
	}
	if workspace != nil {
		m.schemaStatus = workspace.schemaStatus
		m.schemaVersion = workspace.schemaVersion
		workspace.maintenance = m
	}
	return m
}
func (m *DiagnosticMaintenance) Close() {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.jobs.Wait()
}
func (m *DiagnosticMaintenance) ExportDiagnostics() (result v1.ExportResult) {
	result.Status = "cancelled"
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if m.busy {
		m.mu.Unlock()
		id := diagnostics.ID()
		return v1.ExportResult{Status: "failed", ErrorID: &id}
	}
	m.busy = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		if recover() != nil {
			result = m.failure("panic")
		}
	}()
	path, err := m.pick()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if err != nil {
		m.mu.Unlock()
		return m.failure("io")
	}
	if path == "" {
		m.mu.Unlock()
		return
	}
	m.jobs.Add(1)
	m.mu.Unlock()
	defer m.jobs.Done()
	snap, err := m.diag.Collect(m.ctx)
	if err == nil {
		manifest := m.diag.Manifest(m.schemaStatus, m.schemaVersion, snap)
		err = publishDiagnosticArchive(m.ctx, path, m.protected, func(w io.Writer) error { return diagnostics.WriteArchive(m.ctx, w, manifest, snap) })
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		return m.failure("io")
	}
	return v1.ExportResult{Status: "exported", Coverage: &snap.Coverage}
}
func (m *DiagnosticMaintenance) failure(class string) v1.ExportResult {
	id := m.diag.Fault("wailsread", "diagnostics.export", diagnostics.ID(), class)
	return v1.ExportResult{Status: "failed", ErrorID: &id}
}
