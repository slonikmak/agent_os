#!/bin/sh
set -eu
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_CFLAGS=-mmacosx-version-min=15.0
export CGO_LDFLAGS=-mmacosx-version-min=15.0
# Requires pinned Go, Node and Wails on PATH; run from the repository root.
wails3 generate bindings -f '-tags bindings' -ts -i
(cd frontend && npm ci && npm run typecheck && npm test && npm run build)
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go test -race ./...
go build -tags production -trimpath -o bin/personal-desk .
