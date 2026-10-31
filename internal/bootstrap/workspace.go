// Package bootstrap owns the workspace lock, readiness and resource lifetime.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/httpapi"
	materialfiles "personaldesk/internal/adapters/materials"
	"personaldesk/internal/adapters/sqlite"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/materials"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
	"personaldesk/migrations"
)

var errPrivate = errors.New("private workspace setup failed")
var errOwned = errors.New("workspace already owned")

type Config struct {
	Root              string
	Address           string
	retainDiagnostics bool
}
type StartupError struct {
	Code      string
	ErrorID   string
	RequestID string
}

func (e *StartupError) Error() string { return "Workspace startup failed (" + e.Code + ")" }

type Credentials struct {
	Reader      string `json:"reader"`
	Worker      string `json:"worker"`
	Coordinator string `json:"coordinator"`
}
type Discovery struct {
	Endpoint   string `json:"endpoint"`
	APIVersion string `json:"apiVersion"`
}
type Workspace struct {
	maintenance     *DiagnosticMaintenance
	schemaStatus    string
	schemaVersion   *int64
	root            string
	lock            *os.File
	store           *sqlite.Store
	materials       *materialfiles.Content
	server          *http.Server
	listener        net.Listener
	serveDone       chan struct{}
	cancel          context.CancelFunc
	lifetime        context.Context
	closeOnce       sync.Once
	domainCloseOnce sync.Once
	diag            *diagnostics.Buffer
	reader          queries.Reader
	closeErr        error
	discovery       bool
	changesMu       sync.Mutex
	subscribers     map[uint64]func(int64)
	nextSubscriber  uint64
}

func ResolveRoot(override string) (string, error) {
	if override == "" {
		return defaultRoot()
	}
	if !filepath.IsAbs(override) || strings.TrimSpace(override) != override {
		return "", errPrivate
	}
	return filepath.Clean(override), nil
}

// StartSession retains an owned diagnostic session on startup failure for the
// native error shell. Its caller must Close the returned session even on error.
func StartSession(ctx context.Context, config Config, diag *diagnostics.Buffer) (*Workspace, error) {
	config.retainDiagnostics = true
	return Start(ctx, config, diag)
}
func Start(ctx context.Context, config Config, diag *diagnostics.Buffer) (_ *Workspace, result error) {
	defer func() {
		var startup *StartupError
		if errors.As(result, &startup) && startup.ErrorID == "" && startup.Code != "workspace_owned" {
			startup.RequestID = diagnostics.ID()
			startup.ErrorID = diag.Fault("bootstrap", "startup", startup.RequestID, "internal")
		}
	}()
	catalog, err := migrations.Embedded()
	if err != nil {
		return nil, &StartupError{Code: "catalog_invalid"}
	}
	return startCatalog(ctx, config, diag, catalog, recoveryIO{})
}
func startCatalog(ctx context.Context, config Config, diag *diagnostics.Buffer, catalog *migrations.Catalog, recovery recoveryIO) (session *Workspace, result error) {
	root, err := ResolveRoot(config.Root)
	if err != nil {
		return nil, &StartupError{Code: "workspace_unsafe"}
	}
	if err = validateAncestors(root); err != nil {
		return nil, &StartupError{Code: "workspace_unsafe"}
	}
	if err = privateRoot(root); err != nil {
		return nil, &StartupError{Code: "workspace_unsafe"}
	}
	lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
	if err != nil {
		code := "workspace_unsafe"
		if errors.Is(err, errOwned) {
			code = "workspace_owned"
		}
		return nil, &StartupError{Code: code}
	}
	lifetime, cancel := context.WithCancel(context.Background())
	w := &Workspace{schemaStatus: "unknown", root: root, lock: lock, diag: diag, lifetime: lifetime, cancel: cancel, subscribers: make(map[uint64]func(int64))}
	defer func() {
		if recover() != nil {
			result = &StartupError{Code: "service_unavailable"}
			diag.RequireRestart()
		}
		if result != nil {
			var startup *StartupError
			if errors.As(result, &startup) && startup.ErrorID == "" {
				startup.RequestID = diagnostics.ID()
				startup.ErrorID = diag.Fault("bootstrap", "startup", startup.RequestID, "internal")
			}
			w.stopDomain()
			if config.retainDiagnostics {
				session = w
			} else {
				_ = w.Close()
			}
		}
	}()
	attachDiagnostics(root, diag)
	diag.Record("app.start", "startup", "", "")
	// A durable replacement intent gates startup immediately after ownership,
	// before opening or even creating the SQLite main file.
	if err = recoveryPending(root); err != nil {
		return nil, &StartupError{Code: "recovery_required"}
	}
	// Inspect every service path before SQLite can follow/open a private file.
	for _, name := range []string{"workspace.sqlite", "workspace.sqlite-wal", "workspace.sqlite-shm", "credentials.json", "discovery.json"} {
		path := filepath.Join(root, name)
		if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		f, err := openPrivate(path, os.O_RDONLY)
		if err != nil {
			return nil, &StartupError{Code: "workspace_unsafe"}
		}
		f.Close()
	}
	// A previous unclean exit may leave discovery. Ownership permits removing
	// it now; failed startup must never advertise a stale endpoint.
	if err = os.Remove(filepath.Join(root, "discovery.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, &StartupError{Code: "discovery_unsafe"}
	}
	file, err := openPrivate(filepath.Join(root, "workspace.sqlite"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, &StartupError{Code: "workspace_unsafe"}
	}
	file.Close()
	startupCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	w.store, err = sqlite.OpenCatalogObserved(startupCtx, filepath.Join(root, "workspace.sqlite"), catalog, func(ctx context.Context, state sqlite.State) error {
		return ensureUpgradeBackup(ctx, root, catalog, state, recovery)
	}, func(_ sqlite.MigrationStage, duration time.Duration, ok bool) {
		ms := duration.Milliseconds()
		if ms < 0 {
			ms = 0
		}
		if ms > 86400000 {
			ms = 86400000
		}
		event := diagnostics.Event{Level: "info", Code: "migration.step", Component: "database", Operation: "migration", DurationMs: &ms, Frames: diagnostics.Frames()}
		if !ok {
			event.Level = "error"
			event.ErrorClass = "database"
		}
		diag.Capture(event)
	})
	if err != nil {
		w.schemaStatus = "unavailable"
		code := "database_unavailable"
		if errors.Is(err, sqlite.ErrUnsupported) {
			code = "database_unsupported"
			w.schemaStatus = "unsupported"
		}
		return nil, &StartupError{Code: code}
	}
	w.schemaStatus = "ready"
	applied := catalog.Latest()
	w.schemaVersion = &applied
	if err = finishUpgrade(root, catalog.Identity(), recovery); err != nil {
		return nil, &StartupError{Code: "recovery_required"}
	}
	diag.Record("db.init", "startup", "", "")
	w.materials, err = materialfiles.Open(root)
	if err != nil && !errors.Is(err, materialfiles.ErrUnsupported) {
		return nil, &StartupError{Code: "workspace_unsafe"}
	}
	credentials, err := loadCredentials(filepath.Join(root, "credentials.json"))
	if err != nil {
		return nil, &StartupError{Code: "credentials_unsafe"}
	}
	address := config.Address
	if address == "" {
		address = "127.0.0.1:8765"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return nil, &StartupError{Code: "endpoint_invalid"}
	}
	w.listener, err = net.Listen("tcp4", address)
	if err != nil {
		return nil, &StartupError{Code: "endpoint_occupied"}
	}
	w.reader = queries.New(w.store)
	if w.materials != nil {
		w.reader = queries.NewWithMaterials(w.store, w.materials)
	}
	auth, err := access.New(credentials.Callers())
	if err != nil {
		return nil, &StartupError{Code: "credentials_unsafe"}
	}
	endpoint := "http://" + w.listener.Addr().String()
	handler := httpapi.NewCommands(w.reader, auth, w.listener.Addr().String(), diag, intake.NewReceiver(w.store, w.publishRevision), intake.NewTaskCreator(w.store, w.publishRevision))
	if w.materials != nil {
		handler = httpapi.NewMaterials(w.reader, auth, w.listener.Addr().String(), diag, intake.NewReceiver(w.store, w.publishRevision), intake.NewTaskCreator(w.store, w.publishRevision), materials.New(w.store, w.materials, w.publishRevision))
	}
	w.server = &http.Server{ErrorLog: log.New(io.Discard, "", 0), Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: v1.MaxHeaderBytes, BaseContext: func(net.Listener) context.Context { return lifetime }}
	w.serveDone = make(chan struct{})
	go func() {
		defer close(w.serveDone)
		defer func() {
			if recover() != nil {
				diag.Fault("bootstrap", "framework", diagnostics.ID(), "panic")
				diag.RequireRestart()
				cancel()
			}
		}()
		if err := w.server.Serve(w.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			diag.Fault("bootstrap", "startup", diagnostics.ID(), "internal")
			diag.RequireRestart()
			cancel()
		}
	}()
	w.discovery = true
	if err = writeDiscovery(filepath.Join(root, "discovery.json"), Discovery{endpoint, "v1"}); err != nil {
		return nil, &StartupError{Code: "discovery_unsafe"}
	}
	return w, nil
}

// Reject path aliases before creating any owned state. This also rejects UNC
// workspaces on Windows, keeping this process's persistence on a local volume.
func validateAncestors(root string) error {
	if strings.HasPrefix(root, `\\`) {
		return errPrivate
	}
	for path := root; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errPrivate
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
	}
}
func loadCredentials(path string) (Credentials, error) {
	var credentials Credentials
	f, err := openPrivate(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		for {
			var tokens [3]string
			for i := range tokens {
				var raw [32]byte
				if _, err := rand.Read(raw[:]); err != nil {
					return Credentials{}, err
				}
				tokens[i] = hex.EncodeToString(raw[:])
			}
			credentials = Credentials{tokens[0], tokens[1], tokens[2]}
			if _, err := access.New(credentials.Callers()); err == nil {
				break
			}
		}
		f, err = openPrivate(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
		if err != nil {
			return credentials, err
		}
		err = json.NewEncoder(f).Encode(credentials)
		if err == nil {
			err = f.Sync()
		}
		return credentials, errors.Join(err, f.Close())
	}
	if err != nil {
		return credentials, err
	}
	defer f.Close()
	credentials, err = v1.Decode[Credentials]("credentials", f, v1.MaxCredentialBytes)
	if err != nil {
		return Credentials{}, errPrivate
	}
	if _, err = access.New(credentials.Callers()); err != nil {
		return Credentials{}, errPrivate
	}
	return credentials, nil
}
func (c Credentials) Callers() []access.Credential {
	return []access.Credential{{Role: access.Reader, Token: c.Reader}, {Role: access.Worker, Token: c.Worker}, {Role: access.Coordinator, Token: c.Coordinator}}
}
func writeDiscovery(path string, discovery Discovery) error {
	f, err := openPrivate(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Truncate(0); err != nil {
		return err
	}
	if err = json.NewEncoder(f).Encode(discovery); err != nil {
		return err
	}
	return f.Sync()
}
func (w *Workspace) GetWorkspaceSnapshot(ctx context.Context) (queries.WorkspaceSnapshot, error) {
	select {
	case <-w.lifetime.Done():
		return queries.WorkspaceSnapshot{}, queries.Unavailable()
	default:
	}
	merged, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.lifetime, cancel)
	defer cancel()
	defer stop()
	return w.reader.GetWorkspaceSnapshot(merged)
}
func (w *Workspace) stopDomain() {
	w.domainCloseOnce.Do(func() {
		// Cancel all in-flight writes/queries before draining the API. No owned
		// callback may publish after subscriptions close or ownership is released.
		w.cancel()
		w.changesMu.Lock()
		clear(w.subscribers)
		w.changesMu.Unlock()
		if w.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			w.closeErr = w.server.Shutdown(ctx)
			cancel()
			if w.closeErr != nil {
				_ = w.server.Close()
			}
		}
		w.cancel()
		if w.listener != nil {
			_ = w.listener.Close()
		}
		if w.serveDone != nil {
			<-w.serveDone
		}
		if w.discovery {
			w.closeErr = errors.Join(w.closeErr, os.Remove(filepath.Join(w.root, "discovery.json")))
		}
		if w.store != nil {
			w.closeErr = errors.Join(w.closeErr, w.store.Close())
		}
		if w.materials != nil {
			w.closeErr = errors.Join(w.closeErr, w.materials.Close())
		}
	})
}

// Halt gates unsafe work without reentering callback/shutdown locks.
func (w *Workspace) Halt() { w.diag.RequireRestart(); w.cancel() }
func (w *Workspace) Close() error {
	w.closeOnce.Do(func() {
		if w.maintenance != nil {
			w.maintenance.Close()
		}
		w.stopDomain()
		w.diag.Record("app.stop", "shutdown", "", "")
		w.diag.Close() // drains and marks clean while the OS lock is still held
		w.closeErr = errors.Join(w.closeErr, releaseLock(w.lock))
	})
	return w.closeErr
}
