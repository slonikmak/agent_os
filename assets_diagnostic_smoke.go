//go:build diagnostic_smoke && !bindings

package main

import "embed"

// Test-only independent asset graph. No fault endpoint, binding or production
// environment switch exists. The normal bundle always uses assets.go.
//
//go:embed all:frontend/dist-diagnostic-smoke
var assets embed.FS
