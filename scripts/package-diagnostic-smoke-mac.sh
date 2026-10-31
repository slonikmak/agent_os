#!/bin/sh
set -eu
# Separate native test fixture, never changes the production bundle/assets.
(cd frontend && npm exec vite build -- --mode diagnostic-smoke)
mv frontend/dist-diagnostic-smoke/diagnostic-smoke/index.html frontend/dist-diagnostic-smoke/index.html
rmdir frontend/dist-diagnostic-smoke/diagnostic-smoke
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_CFLAGS=-mmacosx-version-min=15.0
export CGO_LDFLAGS=-mmacosx-version-min=15.0
bundle="bin/Personal Desk Diagnostic Fixture.app"
mkdir -p "$bundle/Contents/MacOS"
go build -tags 'production diagnostic_smoke' -trimpath -o "$bundle/Contents/MacOS/personal-desk" .
cp build/darwin/Info.plist "$bundle/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set :CFBundleIdentifier personal.personaldesk.desktop.diagnostic-fixture' "$bundle/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set :CFBundleName Personal Desk Diagnostic Fixture' "$bundle/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set :CFBundleDisplayName Personal Desk Diagnostic Fixture' "$bundle/Contents/Info.plist"
codesign --force --sign - "$bundle"
codesign --verify --strict "$bundle"
