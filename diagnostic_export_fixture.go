//go:build diagnostic_smoke

package main

import (
	"encoding/json"
	"os"
	"time"

	"personaldesk/internal/adapters/diagnostics"
	v1 "personaldesk/internal/contracts/v1"
)

// Native fixture-only injection. These environment switches and synthetic
// controls do not compile into normal production binaries or assets.
func diagnosticSaveFixture(native func() (string, error)) func() (string, error) {
	if destination := os.Getenv("PERSONAL_DESK_EXPORT_FIXTURE_DESTINATION"); destination != "" {
		return func() (string, error) {
			if destination == "cancel" {
				return "", nil
			}
			return destination, nil
		}
	}
	return native
}
func diagnosticFixtureURL() string {
	if os.Getenv("PERSONAL_DESK_EXPORT_FIXTURE_MODE") == "render" {
		return "/?diagnosticRenderFault=1"
	}
	return "/"
}
func runDiagnosticExportFixture(b *diagnostics.Buffer, export func() v1.ExportResult) {
	if os.Getenv("PERSONAL_DESK_EXPORT_FIXTURE_DESTINATION") == "" {
		return
	}
	go func() {
		if os.Getenv("PERSONAL_DESK_EXPORT_FIXTURE_MODE") == "render" {
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				found := false
				for _, event := range b.Snapshot() {
					if event.Code == "ui.render_fault" {
						found = true
						break
					}
				}
				if found {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		_ = json.NewEncoder(os.Stdout).Encode(export())
	}()
}
