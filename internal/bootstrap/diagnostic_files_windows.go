package bootstrap

import "os"

// Windows persistence stays disabled until a directory-handle adapter and native
// alias/reparse/replacement tests can establish the same confinement as Mac.
// These stubs deliberately never open a pathname; bootstrap retains the bounded
// private diagnostic ring/reporting and exposes degraded/shared-unavailable.
type diagnosticDirectory struct{}

func openDiagnosticDirectory(string) (*diagnosticDirectory, error)      { return nil, errPrivate }
func openPrivateIn(*diagnosticDirectory, string, int) (*os.File, error) { return nil, errPrivate }
func validateDiagnosticFile(*os.File) error                             { return errPrivate }
func (*diagnosticDirectory) Sync() error                                { return errPrivate }
func (*diagnosticDirectory) Close() error                               { return nil }
func (*diagnosticDirectory) Remove(string) error                        { return errPrivate }
func (*diagnosticDirectory) Rename(string, string) error                { return errPrivate }
