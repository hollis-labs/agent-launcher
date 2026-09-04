#!/usr/bin/env bash

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: the packaged tray check requires macOS" >&2
  exit 2
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
app_path=${1:-"$repo_root/build/bin/Tachyon.app"}

if [[ ! -d "$app_path" ]]; then
  echo "error: app bundle not found: $app_path" >&2
  exit 2
fi
app_path=$(cd "$app_path" && pwd -P)
executable="$app_path/Contents/MacOS/Tachyon"

if [[ ! -x "$executable" ]]; then
  echo "error: build the package first: ./scripts/build-macos.sh" >&2
  exit 2
fi

find_exact_pid() {
  local candidate
  while IFS= read -r candidate; do
    if [[ "$(ps -ww -p "$candidate" -o command= 2>/dev/null || true)" == "$executable" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done < <(pgrep -x Tachyon || true)
  return 1
}

if existing_pid=$(find_exact_pid); then
  echo "error: exact package is already running as PID $existing_pid; quit it before this isolated check" >&2
  exit 2
fi

tmp_root=${TMPDIR:-/tmp}
capture_dir=$(mktemp -d "$tmp_root/tachyon-menubarcheck.XXXXXX")
launched_pid=""

cleanup() {
  if [[ -n "$launched_pid" ]] &&
    [[ "$(ps -ww -p "$launched_pid" -o command= 2>/dev/null || true)" == "$executable" ]]; then
    kill -TERM "$launched_pid"
    for _ in {1..50}; do
      if ! kill -0 "$launched_pid" 2>/dev/null; then
        break
      fi
      sleep 0.1
    done
  fi
  rm -rf -- "$capture_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

(cd "$repo_root" && go build -o "$capture_dir/tachyon-menubarcheck" ./cmd/tachyon-menubarcheck)

open -n "$app_path"
for _ in {1..100}; do
  if launched_pid=$(find_exact_pid); then
    break
  fi
  sleep 0.1
done
if [[ -z "$launched_pid" ]]; then
  echo "error: LaunchServices did not start the exact packaged executable" >&2
  exit 1
fi

item_details=""
for _ in {1..100}; do
  if item_details=$(osascript - "$launched_pid" <<'APPLESCRIPT' 2>/dev/null
on run argv
  set wantedPID to item 1 of argv as integer
  tell application "System Events"
    set appProcess to first application process whose unix id is wantedPID
    set statusItem to menu bar item 1 of menu bar 2 of appProcess
    set {itemX, itemY} to position of statusItem
    set {itemWidth, itemHeight} to size of statusItem
    return (name of statusItem as text) & "|" & itemX & "|" & itemY & "|" & itemWidth & "|" & itemHeight
  end tell
end run
APPLESCRIPT
  ); then
    break
  fi
  sleep 0.1
done
if [[ -z "$item_details" ]]; then
  echo "error: could not resolve Tachyon's status item; grant Accessibility access to the invoking terminal" >&2
  exit 1
fi

IFS='|' read -r item_name item_x item_y item_width item_height <<<"$item_details"
if [[ "$item_name" != "⌁" || "$item_width" -le 0 || "$item_height" -le 0 ]]; then
  echo "error: unexpected status item: name=$item_name frame=$item_x:$item_y:$item_width:$item_height" >&2
  exit 1
fi

capture_path="$capture_dir/status-item.png"
check_output=""
mark_visible=false
for _ in {1..50}; do
  screencapture -x -R"$item_x,$item_y,$item_width,$item_height" "$capture_path"
  if check_output=$("$capture_dir/tachyon-menubarcheck" "$capture_path" 2>&1); then
    mark_visible=true
    break
  fi
  sleep 0.1
done
if [[ "$mark_visible" != true ]]; then
  echo "$check_output" >&2
  exit 1
fi
echo "$check_output"

echo "verified packaged tray: pid=$launched_pid name=$item_name frame=$item_x:$item_y:$item_width:$item_height"
