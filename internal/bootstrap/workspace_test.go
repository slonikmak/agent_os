package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"personaldesk/internal/application/access"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/wailsread"
	v1 "personaldesk/internal/contracts/v1"
)

func newRoot(t *testing.T) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "workspace")
}
func start(t *testing.T, root string) *Workspace {
	t.Helper()
	w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}
func discovery(t *testing.T, root string) Discovery {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "discovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d Discovery
	if err = json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "reader") {
		t.Fatal("discovery contains credentials")
	}
	return d
}
func TestNativeAndHTTPReadSameWorkspaceRestartAndQuit(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	native := wailsread.New(w, diagnostics.New(), "").GetWorkspaceSnapshot(context.Background())
	if native.Error != nil {
		t.Fatal(native.Error)
	}
	credentials, err := loadCredentials(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	originalCredentials, _ := os.ReadFile(filepath.Join(root, "credentials.json"))
	endpoint := discovery(t, root).Endpoint
	req, _ := http.NewRequest("GET", endpoint+"/api/v1/workspace", nil)
	req.Header.Set("Authorization", "Bearer "+credentials.Reader)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	encoded, _ := json.Marshal(native.Data)
	if strings.TrimSpace(string(body)) != string(encoded) {
		t.Fatalf("HTTP and native differ: %s; %s", body, encoded)
	}
	var httpSnapshot v1.WorkspaceSnapshot
	if err = json.Unmarshal(body, &httpSnapshot); err != nil || httpSnapshot.Counts.Tasks != 0 || httpSnapshot.WorkspaceRevision != 1 {
		t.Fatal("not genuine empty snapshot")
	}
	// Passive rereads cannot create domain records or advance the revision.
	for range 5 {
		next := wailsread.New(w, diagnostics.New(), "").GetWorkspaceSnapshot(context.Background())
		if !reflect.DeepEqual(native.Data, next.Data) {
			t.Fatal("viewing changed canonical snapshot")
		}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err = os.Stat(filepath.Join(root, "discovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("discovery still advertised")
	}
	client := http.Client{Timeout: time.Second}
	if response, err := client.Get(endpoint + "/api/v1"); err == nil {
		response.Body.Close()
		t.Fatal("listener survived quit")
	}
	again := start(t, root)
	restarted := wailsread.New(again, diagnostics.New(), "").GetWorkspaceSnapshot(context.Background())
	if !reflect.DeepEqual(native.Data, restarted.Data) {
		t.Fatal("restart changed metadata")
	}
	after, _ := os.ReadFile(filepath.Join(root, "credentials.json"))
	if !reflect.DeepEqual(originalCredentials, after) {
		t.Fatal("restart changed credentials")
	}
}
func TestCompetingOwnerCannotTouchWorkspace(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	paths := []string{"workspace.sqlite", "credentials.json", "discovery.json"}
	hashes := map[string][32]byte{}
	for _, name := range paths {
		data, _ := os.ReadFile(filepath.Join(root, name))
		hashes[name] = sha256.Sum256(data)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestLockProcessHelper$")
	command.Env = append(os.Environ(), "PERSONAL_DESK_LOCK_HELPER="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper=%s error=%v", output, err)
	}
	for _, name := range paths {
		data, _ := os.ReadFile(filepath.Join(root, name))
		if hashes[name] != sha256.Sum256(data) {
			t.Fatalf("second process altered %s", name)
		}
	}
	if _, err := w.GetWorkspaceSnapshot(context.Background()); err != nil {
		t.Fatal("owner stopped")
	}
}
func TestLockProcessHelper(t *testing.T) {
	root := os.Getenv("PERSONAL_DESK_LOCK_HELPER")
	if root == "" {
		return
	}
	w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
	if w != nil {
		w.Close()
	}
	var startup *StartupError
	if !errors.As(err, &startup) || startup.Code != "workspace_owned" {
		t.Fatalf("competing start=%v", err)
	}
}
func TestFailedStartupReleasesOwnershipAndDoesNotResetUnknownDB(t *testing.T) {
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	file, err := openPrivate(filepath.Join(root, "workspace.sqlite"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "workspace.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE unknown(secret TEXT);INSERT INTO unknown VALUES('SECRET')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
	stale, err := openPrivate(filepath.Join(root, "discovery.json"), os.O_CREATE|os.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stale.WriteString(`{"endpoint":"http://127.0.0.1:1","apiVersion":"v1"}`)
	stale.Close()
	if err != nil {
		t.Fatal(err)
	}
	w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
	if w != nil {
		w.Close()
	}
	var startup *StartupError
	if !errors.As(err, &startup) || startup.Code != "database_unsupported" {
		t.Fatalf("failure=%v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "workspace.sqlite"))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unknown database changed")
	}
	lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
	if err != nil {
		t.Fatal("failed startup retained lock")
	}
	releaseLock(lock)
	if _, err = os.Stat(filepath.Join(root, "discovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed startup advertised API")
	}
}
func TestOccupiedPortIsExplicitAndUnchanged(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	root := newRoot(t)
	w, err := Start(context.Background(), Config{Root: root, Address: occupied.Addr().String()}, diagnostics.New())
	if w != nil {
		w.Close()
	}
	var startup *StartupError
	if !errors.As(err, &startup) || startup.Code != "endpoint_occupied" {
		t.Fatalf("failure=%v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "discovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed startup advertised API")
	}
	lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
	if err != nil {
		t.Fatal(err)
	}
	releaseLock(lock)
}
func TestUnsafeFilesAndSymlinkSetupRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode/symlink cases; Windows ACL behavior requires native acceptance")
	}
	for _, name := range []string{"workspace.lock", "workspace.sqlite", "credentials.json", "discovery.json"} {
		t.Run(name, func(t *testing.T) {
			root := newRoot(t)
			if err := privateRoot(root); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("KEEP"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			if w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New()); err == nil {
				w.Close()
				t.Fatal("accepted symlink")
			}
			data, _ := os.ReadFile(target)
			if string(data) != "KEEP" {
				t.Fatal("symlink target changed")
			}
		})
	}
	root := newRoot(t)
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if w, err := Start(context.Background(), Config{Root: root}, diagnostics.New()); err == nil {
		w.Close()
		t.Fatal("accepted public root")
	}
	if _, err := ResolveRoot("relative"); err == nil {
		t.Fatal("relative override silently accepted")
	}
}

func TestSymlinkAncestorRejectedBeforeCreatingState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink test; native Windows reparse acceptance is pending")
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err = os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if w, err := Start(context.Background(), Config{Root: filepath.Join(alias, "workspace"), Address: "127.0.0.1:0"}, diagnostics.New()); err == nil {
		w.Close()
		t.Fatal("accepted an ancestor alias")
	}
	if _, err = os.Stat(filepath.Join(target, "workspace")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created state through alias")
	}
}
func TestInvalidCredentialsStayIntact(t *testing.T) {
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials.json")
	file, err := openPrivate(path, os.O_CREATE|os.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(`{"reader":"PRIVATE INVALID TOKEN"}`)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	w, err := Start(context.Background(), Config{Root: root, Address: "127.0.0.1:0"}, diagnostics.New())
	if w != nil {
		w.Close()
	}
	var startup *StartupError
	if !errors.As(err, &startup) || startup.Code != "credentials_unsafe" {
		t.Fatalf("startup=%v", err)
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid credentials silently reset")
	}
}

func TestStrictCredentialFilesRetainValidTokensAndRejectEveryAmbiguousForm(t *testing.T) {
	valid := Credentials{Reader: strings.Repeat("1", 64), Worker: strings.Repeat("2", 64), Coordinator: strings.Repeat("3", 64)}
	raw, _ := json.Marshal(valid)
	cases := map[string][]byte{
		"duplicate":                 []byte(strings.Replace(string(raw), `"reader":`, `"reader":"`+valid.Worker+`","reader":`, 1)),
		"case-alias":                []byte(strings.Replace(string(raw), `"reader":`, `"Reader":`, 1)),
		"case-alias-extra":          []byte(strings.Replace(string(raw), `"reader":`, `"Reader":"`+valid.Reader+`","reader":`, 1)),
		"unknown":                   []byte(strings.Replace(string(raw), `"reader":`, `"secret":"PRIVATE","reader":`, 1)),
		"same-token":                []byte(strings.Replace(string(raw), valid.Worker, valid.Reader, 1)),
		"uppercase-token":           []byte(strings.Replace(string(raw), valid.Worker, strings.Repeat("A", 64), 1)),
		"short-token":               []byte(strings.Replace(string(raw), valid.Worker, "PRIVATE", 1)),
		"null":                      []byte(strings.Replace(string(raw), `"`+valid.Worker+`"`, `null`, 1)),
		"trailing":                  append(append([]byte{}, raw...), []byte(` {}`)...),
		"invalid-utf8":              append(append([]byte{}, raw...), 0xff),
		"oversized-whitespace-tail": append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), 4097)...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			root := newRoot(t)
			if err := privateRoot(root); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "credentials.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := loadCredentials(path)
			if err == nil || got != (Credentials{}) {
				t.Fatal("invalid credential file accepted/partially decoded")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(data, after) {
				t.Fatal("invalid credentials rewritten")
			}
		})
	}
	root := newRoot(t)
	if err := privateRoot(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials.json")
	created, err := loadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := loadCredentials(path)
	if err != nil || created != again {
		t.Fatal("new credentials not reusable")
	}
	if _, err = access.New(created.Callers()); err != nil {
		t.Fatal("new credentials lack distinct valid roles")
	}
}
