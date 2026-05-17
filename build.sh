#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST="$ROOT/dist"
APP="$DIST/Tachyon.app"
ENGINE_BIN="$APP/Contents/Resources/tachyon-engine"
APP_BIN="$APP/Contents/MacOS/Tachyon"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

(
  cd "$ROOT/engine"
  GOWORK=off go build -o "$ENGINE_BIN" .
)

cp "$ROOT/app/Info.plist" "$APP/Contents/Info.plist"

swiftc \
  -O \
  -framework AppKit \
  -framework Carbon \
  "$ROOT/app/main.swift" \
  -o "$APP_BIN"

echo "Built $APP"

