package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/sqlite"
	"personaldesk/migrations"
)

var errRecovery = errors.New("workspace recovery required")

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type backupManifest struct {
	Format        int    `json:"format"`
	ID            string `json:"id"`
	SchemaVersion int64  `json:"schemaVersion"`
	Prefix        string `json:"prefix"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	Kind          string `json:"kind"`
	Target        string `json:"target,omitempty"`
	CreatedAt     string `json:"createdAt"`
}
type upgradeIntent struct {
	Format        int    `json:"format"`
	BackupID      string `json:"backupId"`
	Target        string `json:"target"`
	SourceVersion int64  `json:"sourceVersion"`
}
type fileInventory struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	SHA256  string `json:"sha256,omitempty"`
	Size    int64  `json:"size,omitempty"`
}
type restoreIntent struct {
	Format        int             `json:"format"`
	ID            string          `json:"id"`
	BackupID      string          `json:"backupId"`
	SHA256        string          `json:"sha256"`
	Size          int64           `json:"size"`
	SchemaVersion int64           `json:"schemaVersion"`
	Prefix        string          `json:"prefix"`
	Files         []fileInventory `json:"files"`
}

// recoveryIO is per invocation. Tests inject failures or terminate subprocesses
// at durable boundaries without adding production flags or global hooks.
type recoveryIO struct {
	boundary      func(string) error
	syncDirectory func(string) error
}

func (r recoveryIO) syncDir(path string) error {
	if r.syncDirectory != nil {
		return r.syncDirectory(path)
	}
	return syncDir(path)
}

func (r recoveryIO) reached(name string) error {
	if r.boundary != nil {
		return r.boundary(name)
	}
	return nil
}
func validID(id string) bool  { return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == "" }
func validHash(s string) bool { return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == "" }
func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func recoveryPending(root string) error {
	present, err := exists(filepath.Join(root, "restore-intent.json"))
	if err != nil || present {
		return errRecovery
	}
	return nil
}
func readJSON(path string, value any) error {
	f, err := openPrivate(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	if err = d.Decode(value); err != nil {
		return errRecovery
	}
	if d.Decode(new(any)) != io.EOF {
		return errRecovery
	}
	return nil
}
func writeJSON(path string, value any) error {
	f, err := openPrivate(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(value)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
func digest(ctx context.Context, path string) (string, int64, error) {
	f, err := openPrivate(path, os.O_RDONLY)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, f})
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func matches(ctx context.Context, path, hash string, size int64) error {
	got, n, err := digest(ctx, path)
	if err != nil {
		return err
	}
	if got != hash || n != size {
		return errRecovery
	}
	return nil
}
func serviceFiles(root string) error {
	for _, name := range []string{"workspace.sqlite", "workspace.sqlite-wal", "workspace.sqlite-shm", "credentials.json", "discovery.json"} {
		path := filepath.Join(root, name)
		present, err := exists(path)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		f, err := openPrivate(path, os.O_RDONLY)
		if err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
	}
	return nil
}
func recoveryDirs(root string, r recoveryIO) error {
	for _, name := range []string{"backups", "recovery"} {
		if err := privateRoot(filepath.Join(root, name)); err != nil {
			return err
		}
	}
	return r.syncDir(root)
}
func verifyBackup(ctx context.Context, root, id string, catalog *migrations.Catalog) (backupManifest, error) {
	var m backupManifest
	if !validID(id) {
		return m, errRecovery
	}
	// Validate each service directory before traversing it. openPrivate protects
	// leaf handles; it cannot by itself reject a symlink in an ancestor.
	if err := privateRoot(filepath.Join(root, "backups")); err != nil {
		return m, err
	}
	if err := readJSON(filepath.Join(root, "backups", id+".json"), &m); err != nil {
		return m, err
	}
	if m.Format != 1 || m.ID != id || m.SchemaVersion < 1 || m.SchemaVersion > catalog.Latest() || m.Prefix != catalog.PrefixIdentity(m.SchemaVersion) || !validHash(m.SHA256) || m.Size <= 0 || (m.Kind != "manual" && m.Kind != "upgrade") || (m.Kind == "upgrade" && !validHash(m.Target)) || (m.Kind == "manual" && m.Target != "") {
		return m, errRecovery
	}
	if _, err := time.Parse(time.RFC3339, m.CreatedAt); err != nil {
		return m, errRecovery
	}
	path := filepath.Join(root, "backups", id+".sqlite")
	if err := matches(ctx, path, m.SHA256, m.Size); err != nil {
		return m, err
	}
	state, err := sqlite.InspectImage(ctx, path, catalog)
	if err != nil || state.Fresh || state.Version != m.SchemaVersion {
		return m, errRecovery
	}
	return m, nil
}
func listBackups(ctx context.Context, root string, catalog *migrations.Catalog) ([]backupManifest, error) {
	entries, err := os.ReadDir(filepath.Join(root, "backups"))
	if err != nil {
		return nil, err
	}
	var result []backupManifest
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		m, err := verifyBackup(ctx, root, id, catalog)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt == result[j].CreatedAt {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt < result[j].CreatedAt
	})
	return result, nil
}
func createBackup(ctx context.Context, root, kind string, catalog *migrations.Catalog, r recoveryIO) (backupManifest, error) {
	var m backupManifest
	if err := recoveryDirs(root, r); err != nil {
		return m, err
	}
	id := diagnostics.ID()
	image := filepath.Join(root, "backups", id+".sqlite")
	f, err := openPrivate(image, os.O_CREATE|os.O_EXCL|os.O_RDWR)
	if err != nil {
		return m, err
	}
	if err = f.Close(); err != nil {
		return m, err
	}
	state, err := sqlite.Backup(ctx, filepath.Join(root, "workspace.sqlite"), image, catalog)
	if err != nil {
		return m, err
	}
	hash, size, err := digest(ctx, image)
	if err != nil {
		return m, err
	}
	m = backupManifest{Format: 1, ID: id, SchemaVersion: state.Version, Prefix: catalog.PrefixIdentity(state.Version), SHA256: hash, Size: size, Kind: kind, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if kind == "upgrade" {
		m.Target = catalog.Identity()
	}
	if err = r.syncDir(filepath.Dir(image)); err != nil {
		return m, err
	}
	if err = r.reached("backup-image"); err != nil {
		return m, err
	}
	if err = writeJSON(filepath.Join(root, "backups", id+".json"), m); err != nil {
		return m, err
	}
	if err = r.syncDir(filepath.Dir(image)); err != nil {
		return m, err
	}
	if err = r.reached("backup-manifest"); err != nil {
		return m, err
	}
	_, err = verifyBackup(ctx, root, id, catalog)
	return m, err
}
func ensureUpgradeBackup(ctx context.Context, root string, catalog *migrations.Catalog, state sqlite.State, r recoveryIO) error {
	if err := recoveryDirs(root, r); err != nil {
		return err
	}
	if _, err := listBackups(ctx, root, catalog); err != nil {
		return err
	}
	path := filepath.Join(root, "upgrade-intent.json")
	if present, err := exists(path); err != nil {
		return err
	} else if present {
		v, err := loadUpgradeIntent(path)
		if err != nil {
			return err
		}
		if v.Target != catalog.Identity() || v.SourceVersion > state.Version {
			return errRecovery
		}
		m, err := verifyBackup(ctx, root, v.BackupID, catalog)
		if err != nil {
			return err
		}
		if m.Kind != "upgrade" || m.Target != v.Target || m.SchemaVersion != v.SourceVersion {
			return errRecovery
		}
		return nil
	}
	m, err := createBackup(ctx, root, "upgrade", catalog, r)
	if err != nil {
		return err
	}
	if err = writeJSON(path, upgradeIntent{Format: 1, BackupID: m.ID, Target: catalog.Identity(), SourceVersion: state.Version}); err != nil {
		return err
	}
	return r.syncDir(root)
}

func loadUpgradeIntent(path string) (upgradeIntent, error) {
	var v upgradeIntent
	if err := readJSON(path, &v); err != nil {
		return v, err
	}
	if v.Format != 1 || !validID(v.BackupID) || !validHash(v.Target) || v.SourceVersion < 1 {
		return v, errRecovery
	}
	return v, nil
}

// An original point belongs to one unfinished attempt. Successful migration or
// explicit restore retires that attempt while retaining its immutable record
// and image. Returning to an old schema therefore starts a new recovery cycle.
func finishUpgrade(root, target string, r recoveryIO) error {
	path := filepath.Join(root, "upgrade-intent.json")
	present, err := exists(path)
	if err != nil || !present {
		return err
	}
	v, err := loadUpgradeIntent(path)
	if err != nil {
		return err
	}
	if target != "" && v.Target != target {
		return errRecovery
	}
	if err = privateRoot(filepath.Join(root, "recovery")); err != nil {
		return err
	}
	if err = moveNoReplace(path, filepath.Join(root, "recovery", "upgrade-"+v.BackupID+".json")); err != nil {
		return err
	}
	if err = r.syncDir(filepath.Join(root, "recovery")); err != nil {
		return err
	}
	return r.syncDir(root)
}

func prepareRestore(ctx context.Context, root, id string, catalog *migrations.Catalog, r recoveryIO) (string, error) {
	if err := recoveryPending(root); err != nil {
		return "", err
	}
	m, err := verifyBackup(ctx, root, id, catalog)
	if err != nil {
		return "", err
	}
	if m.SchemaVersion != catalog.Latest() {
		return "", errRecovery
	}
	if err = recoveryDirs(root, r); err != nil {
		return "", err
	}
	restoreID := diagnostics.ID()
	dir := filepath.Join(root, "recovery", restoreID)
	if err = privateRoot(dir); err != nil {
		return "", err
	}
	if err = r.syncDir(filepath.Dir(dir)); err != nil {
		return "", err
	}
	stage := filepath.Join(dir, "staged.sqlite")
	src, err := openPrivate(filepath.Join(root, "backups", id+".sqlite"), os.O_RDONLY)
	if err != nil {
		return "", err
	}
	dst, err := openPrivate(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		src.Close()
		return "", err
	}
	_, err = io.Copy(dst, contextReader{ctx, src})
	err = errors.Join(err, src.Close())
	if err == nil {
		err = dst.Sync()
	}
	err = errors.Join(err, dst.Close())
	if err != nil {
		return "", err
	}
	if err = matches(ctx, stage, m.SHA256, m.Size); err != nil {
		return "", err
	}
	if err = verifyExact(ctx, stage, m.SchemaVersion, catalog); err != nil {
		return "", err
	}
	if err = r.syncDir(dir); err != nil {
		return "", err
	}
	if err = r.reached("restore-stage"); err != nil {
		return "", err
	}
	intent := restoreIntent{Format: 1, ID: restoreID, BackupID: id, SHA256: m.SHA256, Size: m.Size, SchemaVersion: m.SchemaVersion, Prefix: m.Prefix}
	for _, name := range []string{"workspace.sqlite", "workspace.sqlite-wal", "workspace.sqlite-shm"} {
		item := fileInventory{Name: name}
		item.Present, err = exists(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
		if item.Present {
			item.SHA256, item.Size, err = digest(ctx, filepath.Join(root, name))
			if err != nil {
				return "", err
			}
		}
		intent.Files = append(intent.Files, item)
	}
	if err = writeJSON(filepath.Join(root, "restore-intent.json"), intent); err != nil {
		return "", err
	}
	if err = r.syncDir(root); err != nil {
		return "", err
	}
	if err = r.reached("restore-intent"); err != nil {
		return restoreID, err
	}
	return restoreID, resumeRestore(ctx, root, restoreID, catalog, r)
}
func verifyExact(ctx context.Context, path string, version int64, catalog *migrations.Catalog) error {
	state, err := sqlite.InspectImage(ctx, path, catalog)
	if err != nil || state.Fresh || state.Pending || state.Version != version {
		return errRecovery
	}
	return nil
}
func loadIntent(root, id string, catalog *migrations.Catalog) (restoreIntent, error) {
	var v restoreIntent
	if !validID(id) {
		return v, errRecovery
	}
	if err := readJSON(filepath.Join(root, "restore-intent.json"), &v); err != nil {
		return v, err
	}
	return v, validateIntent(v, id, catalog)
}
func validateIntent(v restoreIntent, id string, catalog *migrations.Catalog) error {
	if v.Format != 1 || v.ID != id || !validID(v.BackupID) || !validHash(v.SHA256) || v.Size <= 0 || v.SchemaVersion != catalog.Latest() || v.Prefix != catalog.PrefixIdentity(v.SchemaVersion) || len(v.Files) != 3 {
		return errRecovery
	}
	for i, name := range []string{"workspace.sqlite", "workspace.sqlite-wal", "workspace.sqlite-shm"} {
		f := v.Files[i]
		if f.Name != name || f.Present && (!validHash(f.SHA256) || f.Size < 0) || !f.Present && (f.SHA256 != "" || f.Size != 0) {
			return errRecovery
		}
	}
	return nil
}
func resumeRestore(ctx context.Context, root, id string, catalog *migrations.Catalog, r recoveryIO) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := recoveryDirs(root, r); err != nil {
		return err
	}
	// A repeated explicit resume after successful completion is harmless. The
	// archived intent proves completion; it never starts a second replacement.
	if present, err := exists(filepath.Join(root, "restore-intent.json")); err != nil {
		return err
	} else if !present {
		if !validID(id) {
			return errRecovery
		}
		dir := filepath.Join(root, "recovery", id)
		if err = privateRoot(dir); err != nil {
			return err
		}
		var completed restoreIntent
		if err = readJSON(filepath.Join(dir, "completed.json"), &completed); err != nil {
			return err
		}
		if err = validateIntent(completed, id, catalog); err != nil {
			return err
		}
		if err = verifyExact(ctx, filepath.Join(root, "workspace.sqlite"), completed.SchemaVersion, catalog); err != nil {
			return err
		}
		// The completed restore may have been followed by valid saved work. Finish
		// both syncs and report errors without reviving its obsolete replacement
		// intent. Only an actually active completion may retain that startup gate.
		if err = r.syncDir(dir); err != nil {
			return err
		}
		if err = r.syncDir(root); err != nil {
			return err
		}
		return nil
	}
	v, err := loadIntent(root, id, catalog)
	if err != nil {
		return err
	}
	m, err := verifyBackup(ctx, root, v.BackupID, catalog)
	if err != nil || m.SHA256 != v.SHA256 || m.Size != v.Size || m.SchemaVersion != v.SchemaVersion {
		return errRecovery
	}
	dir := filepath.Join(root, "recovery", id)
	if err = privateRoot(dir); err != nil {
		return err
	}
	stage := filepath.Join(dir, "staged.sqlite")
	staged, err := exists(stage)
	if err != nil {
		return err
	}
	if staged {
		if err = matches(ctx, stage, v.SHA256, v.Size); err != nil {
			return err
		}
		if err = verifyExact(ctx, stage, v.SchemaVersion, catalog); err != nil {
			return err
		}
	}
	for _, f := range v.Files {
		if err = ctx.Err(); err != nil {
			return err
		}
		source := filepath.Join(root, f.Name)
		target := filepath.Join(dir, f.Name)
		sourceExists, err := exists(source)
		if err != nil {
			return err
		}
		targetExists, err := exists(target)
		if err != nil {
			return err
		}
		installed := f.Name == "workspace.sqlite" && !staged && sourceExists && matches(ctx, source, v.SHA256, v.Size) == nil
		if !f.Present {
			if targetExists || sourceExists && !installed {
				return errRecovery
			}
			continue
		}
		if targetExists {
			if err = matches(ctx, target, f.SHA256, f.Size); err != nil {
				return err
			}
			if sourceExists && !installed {
				return errRecovery
			}
		} else {
			if !sourceExists || installed {
				return errRecovery
			}
			if err = matches(ctx, source, f.SHA256, f.Size); err != nil {
				return err
			}
			if err = moveNoReplace(source, target); err != nil {
				return err
			}
			if err = r.reached("move-" + f.Name); err != nil {
				return err
			}
		}
		if err = r.syncDir(root); err != nil {
			return err
		}
		if err = r.syncDir(dir); err != nil {
			return err
		}
		if err = r.reached("quarantine-" + f.Name); err != nil {
			return err
		}
	}
	for _, name := range []string{"workspace.sqlite-wal", "workspace.sqlite-shm"} {
		present, err := exists(filepath.Join(root, name))
		if err != nil || present {
			return errRecovery
		}
	}
	main := filepath.Join(root, "workspace.sqlite")
	if staged {
		if err = moveNoReplace(stage, main); err != nil {
			return err
		}
		if err = r.reached("restore-install-move"); err != nil {
			return err
		}
	}
	if err = matches(ctx, main, v.SHA256, v.Size); err != nil {
		return err
	}
	if err = verifyExact(ctx, main, v.SchemaVersion, catalog); err != nil {
		return err
	}
	f, err := openPrivate(main, os.O_RDWR)
	if err != nil {
		return err
	}
	err = errors.Join(f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err = r.syncDir(root); err != nil {
		return err
	}
	if err = r.syncDir(dir); err != nil {
		return err
	}
	if err = r.reached("restore-installed"); err != nil {
		return err
	}
	if err = finishUpgrade(root, "", r); err != nil {
		return err
	}
	return completeRestore(root, dir, v, r)
}

func retainIntent(root string, v restoreIntent, r recoveryIO) error {
	path := filepath.Join(root, "restore-intent.json")
	present, err := exists(path)
	if err != nil {
		return err
	}
	if present {
		var existing restoreIntent
		if err = readJSON(path, &existing); err != nil {
			return err
		}
		if !reflect.DeepEqual(existing, v) {
			return errRecovery
		}
	} else if err = writeJSON(path, v); err != nil {
		return err
	}
	return r.syncDir(root)
}

func completeRestore(root, dir string, v restoreIntent, r recoveryIO) error {
	completedPath := filepath.Join(dir, "completed.json")
	present, err := exists(completedPath)
	if err != nil {
		return err
	}
	if present {
		var completed restoreIntent
		if err = readJSON(completedPath, &completed); err != nil {
			return err
		}
		if !reflect.DeepEqual(completed, v) {
			return errRecovery
		}
	} else {
		// Keep the original durable startup gate while publishing its completion
		// record. A crash at the no-replace move still leaves normal startup gated.
		temporary := filepath.Join(dir, "completion-intent.json")
		if found, err := exists(temporary); err != nil {
			return err
		} else if found {
			var pending restoreIntent
			if err = readJSON(temporary, &pending); err != nil {
				return err
			}
			if !reflect.DeepEqual(pending, v) {
				return errRecovery
			}
		} else if err = writeJSON(temporary, v); err != nil {
			return err
		}
		if err = moveNoReplace(temporary, completedPath); err != nil {
			return err
		}
		if err = r.reached("restore-completion-move"); err != nil {
			return err
		}
	}
	if err = r.syncDir(dir); err != nil {
		return err
	}
	if err = r.syncDir(root); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(root, "restore-intent.json")); err != nil {
		return err
	}
	if err = r.syncDir(root); err != nil {
		return errors.Join(err, retainIntent(root, v, r))
	}
	return r.reached("restore-complete")
}
