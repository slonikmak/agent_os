package bootstrap

import (
	"context"
	"io"
)

// Destination access fails closed until an anchored Windows implementation and
// its native alias/reparse/replacement matrix have been verified.
func publishDiagnosticArchive(context.Context, string, []protectedDestination, func(io.Writer) error) error {
	return errPrivate
}
