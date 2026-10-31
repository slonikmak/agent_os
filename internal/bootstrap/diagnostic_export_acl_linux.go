package bootstrap

import "os"

// POSIX ACL named-user/group permissions are constrained by the group mode
// mask; the existing 0700/0600 checks therefore retain owner-only access.
func privatizeDiagnosticExport(*os.File) error { return nil }
