//go:build darwin && !cgo

package bootstrap

import "os"

// The native descriptor ACL API is required to establish private publication.
func privatizeDiagnosticExport(*os.File) error { return errPrivate }
