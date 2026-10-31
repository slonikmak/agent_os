//go:build !diagnostic_smoke

package main

import (
	"personaldesk/internal/adapters/diagnostics"
	v1 "personaldesk/internal/contracts/v1"
)

func diagnosticSaveFixture(native func() (string, error)) func() (string, error) { return native }
func diagnosticFixtureURL() string                                               { return "/" }
func runDiagnosticExportFixture(*diagnostics.Buffer, func() v1.ExportResult)     {}
