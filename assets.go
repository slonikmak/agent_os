//go:build !bindings && !diagnostic_smoke

package main

import "embed"

//go:embed all:frontend/dist
var assets embed.FS
