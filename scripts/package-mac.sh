#!/bin/sh
set -eu
# Run from the repository root. No installer, release upload or distribution signing.
bundle="bin/Personal Desk.app"
mkdir -p "$bundle/Contents/MacOS"
cp bin/personal-desk "$bundle/Contents/MacOS/personal-desk"
cp build/darwin/Info.plist "$bundle/Contents/Info.plist"
codesign --force --sign - "$bundle"
codesign --verify --strict "$bundle"
