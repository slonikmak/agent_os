//go:build bindings

package main

import "os"

// Binding generation precedes the first frontend build. This filesystem is
// never shipped: normal native builds always embed the production output.
var assets = os.DirFS("frontend/dist")
