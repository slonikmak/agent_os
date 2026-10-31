package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"personaldesk/migrations"
)

// RunMaintenance is an offline invocation of the same executable. Arguments
// contain operations and registered IDs only; the workspace uses normal root
// resolution. No Store, native application, credentials or HTTP service starts.
func RunMaintenance(ctx context.Context, override string, args []string, output io.Writer) int {
	result, code := maintenance(ctx, override, args, recoveryIO{})
	if code != "" {
		result = map[string]any{"status": "failed", "code": code}
	}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		return 1
	}
	if code != "" {
		return 1
	}
	return 0
}
func maintenance(ctx context.Context, override string, args []string, r recoveryIO) (_ map[string]any, code string) {
	if len(args) == 0 || len(args) > 2 {
		return nil, "invalid_arguments"
	}
	op := args[0]
	if (op == "backup" || op == "list-backups") && len(args) != 1 || (op == "restore" || op == "resume") && (len(args) != 2 || !validID(args[1])) {
		return nil, "invalid_arguments"
	}
	if op != "backup" && op != "list-backups" && op != "restore" && op != "resume" {
		return nil, "invalid_arguments"
	}
	catalog, err := migrations.Embedded()
	if err != nil {
		return nil, "catalog_invalid"
	}
	root, err := ResolveRoot(override)
	if err != nil {
		return nil, "workspace_unsafe"
	}
	if err = validateAncestors(root); err != nil {
		return nil, "workspace_unsafe"
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		return nil, "workspace_unavailable"
	}
	if err = privateRoot(root); err != nil {
		return nil, "workspace_unsafe"
	}
	lock, err := acquireLock(filepath.Join(root, "workspace.lock"))
	if err != nil {
		if errors.Is(err, errOwned) {
			return nil, "workspace_owned"
		}
		return nil, "workspace_unsafe"
	}
	defer func() {
		if releaseLock(lock) != nil {
			code = "recovery_failed"
		}
	}()
	if err = serviceFiles(root); err != nil {
		return nil, "workspace_unsafe"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := map[string]any{"status": "ok", "operation": op}
	if op == "list-backups" {
		if err = recoveryDirs(root, r); err != nil {
			return nil, "recovery_failed"
		}
		backups, err := listBackups(ctx, root, catalog)
		if err != nil {
			return nil, "recovery_failed"
		}
		type item struct {
			ID            string `json:"id"`
			SchemaVersion int64  `json:"schemaVersion"`
		}
		items := make([]item, 0, len(backups))
		for _, m := range backups {
			items = append(items, item{m.ID, m.SchemaVersion})
		}
		if len(items) > 1024 {
			return nil, "recovery_failed"
		}
		result["backups"] = items
		if err = recoveryPending(root); err != nil {
			var v restoreIntent
			if readJSON(filepath.Join(root, "restore-intent.json"), &v) != nil || !validID(v.ID) {
				return nil, "recovery_required"
			}
			result["restoreId"] = v.ID
		}
		return result, ""
	}
	if op != "resume" {
		if err = recoveryPending(root); err != nil {
			return nil, "recovery_required"
		}
	}
	switch op {
	case "backup":
		m, err := createBackup(ctx, root, "manual", catalog, r)
		if err != nil {
			return nil, "recovery_failed"
		}
		result["backupId"] = m.ID
		result["schemaVersion"] = m.SchemaVersion
	case "restore":
		id, err := prepareRestore(ctx, root, args[1], catalog, r)
		if err != nil {
			return nil, "recovery_failed"
		}
		result["restoreId"] = id
	case "resume":
		if err = resumeRestore(ctx, root, args[1], catalog, r); err != nil {
			return nil, "recovery_failed"
		}
		result["restoreId"] = args[1]
	}
	return result, ""
}
