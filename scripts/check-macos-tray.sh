#!/usr/bin/env bash

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: the packaged tray check requires macOS" >&2
  exit 2
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
cold_position=false
if [[ "${1:-}" == "--cold-position" ]]; then
  cold_position=true
  shift
fi
if [[ $# -gt 1 ]]; then
  echo "usage: $0 [--cold-position] [APP_PATH]" >&2
  exit 2
fi
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
position_domain="com.hollislabs.tachyon"
legacy_position_key="NSStatusItem Preferred Position Item-0"
named_position_key="NSStatusItem Preferred Position com.hollislabs.tachyon.main-status-item"
legacy_position_existed=false
named_position_existed=false
legacy_position_value=""
named_position_value=""
positions_restored=false

exact_package_running() {
  [[ -n "$launched_pid" ]] &&
    [[ "$(ps -ww -p "$launched_pid" -o command= 2>/dev/null || true)" == "$executable" ]]
}

wait_for_exact_exit() {
  for _ in {1..100}; do
    if ! exact_package_running; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

stop_launched_package() {
  if ! exact_package_running; then
    return 0
  fi

  # This helper uses NSRunningApplication. It checks the PID's executable URL
  # again in the same native call before requesting ordinary Cocoa termination,
  # so cleanup cannot signal a recycled PID or another Tachyon package.
  if [[ -x "$capture_dir/tachyon-menubarcheck" ]] &&
    "$capture_dir/tachyon-menubarcheck" --terminate "$launched_pid" "$executable" >/dev/null 2>&1 &&
    wait_for_exact_exit; then
    return 0
  fi

  # Preserve bounded cleanup on an already-failing check. TERM and KILL are
  # process fallbacks only; KILL is the last resort if Cocoa and TERM fail.
  if exact_package_running; then
    kill -TERM "$launched_pid"
    if wait_for_exact_exit; then
      return 0
    fi
  fi
  if exact_package_running; then
    kill -KILL "$launched_pid"
    wait_for_exact_exit
  fi
}

restore_positions() {
  if [[ "$cold_position" != true || "$positions_restored" == true ]]; then
    return 0
  fi
  if [[ "$legacy_position_existed" == true ]]; then
    defaults write "$position_domain" "$legacy_position_key" -float "$legacy_position_value"
  else
    defaults delete "$position_domain" "$legacy_position_key" >/dev/null 2>&1 || true
  fi
  if [[ "$named_position_existed" == true ]]; then
    defaults write "$position_domain" "$named_position_key" -float "$named_position_value"
  else
    defaults delete "$position_domain" "$named_position_key" >/dev/null 2>&1 || true
  fi
  positions_restored=true
}

cleanup() {
  local exit_status=$?
  trap - EXIT
  stop_launched_package || true
  restore_positions
  rm -rf -- "$capture_dir"
  exit "$exit_status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ "$cold_position" == true ]]; then
  if legacy_position_value=$(defaults read "$position_domain" "$legacy_position_key" 2>/dev/null); then
    legacy_position_existed=true
  fi
  if named_position_value=$(defaults read "$position_domain" "$named_position_key" 2>/dev/null); then
    named_position_existed=true
  fi
  defaults delete "$position_domain" "$legacy_position_key" >/dev/null 2>&1 || true
  defaults delete "$position_domain" "$named_position_key" >/dev/null 2>&1 || true
fi

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
    set statusItem to first menu bar item of menu bar 2 of appProcess whose name is "⌁"
    set {itemX, itemY} to position of statusItem
    set {itemWidth, itemHeight} to size of statusItem
    return (name of statusItem as text) & "|" & (description of statusItem as text) & "|" & itemX & "|" & itemY & "|" & itemWidth & "|" & itemHeight
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

IFS='|' read -r item_name item_description item_x item_y item_width item_height <<<"$item_details"
if [[ "$item_name" != "⌁" || "$item_description" != "Tachyon" || "$item_width" -le 0 || "$item_height" -le 0 ]]; then
  echo "error: unexpected status item: title=$item_name description=$item_description frame=$item_x:$item_y:$item_width:$item_height" >&2
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

# AXPress is the accessibility equivalent of the ordinary left-click action.
# A fresh package starts with both Tachyon windows hidden, so it must expose
# exactly one visible window after the first press and none after the second.
visible_windows=$(osascript - "$launched_pid" <<'APPLESCRIPT'
on run argv
  set wantedPID to item 1 of argv as integer
  tell application "System Events"
    set appProcess to first application process whose unix id is wantedPID
    set statusItem to first menu bar item of menu bar 2 of appProcess whose name is "⌁"
    perform action "AXPress" of statusItem
    delay 0.4
    set shownAfterOpen to count of windows of appProcess
    perform action "AXPress" of statusItem
    delay 0.4
    set shownAfterClose to count of windows of appProcess
    return (shownAfterOpen as text) & "|" & (shownAfterClose as text)
  end tell
end run
APPLESCRIPT
)
if [[ "$visible_windows" != "1|0" ]]; then
  echo "error: AXPress did not toggle the palette open then closed: $visible_windows" >&2
  exit 1
fi

# Exercise a physical right-click through CoreGraphics, then inspect the menu
# AppKit attached to the status item. This requires the same Accessibility
# permission already needed by the frame/pixel checks above.
"$capture_dir/tachyon-menubarcheck" --right-click \
  "$item_x" "$item_y" "$item_width" "$item_height"
menu_items=""
for _ in {1..30}; do
  if menu_items=$(osascript - "$launched_pid" <<'APPLESCRIPT' 2>/dev/null
on run argv
  set wantedPID to item 1 of argv as integer
  tell application "System Events"
    set appProcess to first application process whose unix id is wantedPID
    set statusItem to first menu bar item of menu bar 2 of appProcess whose name is "⌁"
    return name of every menu item of menu 1 of statusItem
  end tell
end run
APPLESCRIPT
  ); then
    break
  fi
  sleep 0.1
done
if [[ "$menu_items" != *"Open Manager"* || "$menu_items" != *"Settings…"* || "$menu_items" != *"Quit Tachyon"* ]]; then
  osascript -e 'tell application "System Events" to key code 53' >/dev/null
  echo "error: right-click tray menu did not expose the expected actions: $menu_items" >&2
  exit 1
fi

# End a successful check through Tachyon's exact native Quit menu action. This
# requests normal Cocoa termination, which runs Wails' shutdown hooks and lets
# the tray bridge remove its status item and synchronously flush AppKit's
# autosaved position before cold-position restoration begins. The EXIT trap's
# signals are deliberately only a bounded fallback for an already-failing run.
verified_pid=$launched_pid
if ! osascript - "$launched_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set wantedPID to item 1 of argv as integer
  tell application "System Events"
    set appProcess to first application process whose unix id is wantedPID
    set statusItem to first menu bar item of menu bar 2 of appProcess whose name is "⌁"
    click menu item "Quit Tachyon" of menu 1 of statusItem
  end tell
end run
APPLESCRIPT
then
  echo "error: could not request native Quit for exact package PID $launched_pid" >&2
  exit 1
fi
if ! wait_for_exact_exit; then
  echo "error: exact package PID $launched_pid did not finish native Quit" >&2
  exit 1
fi
launched_pid=""
restore_positions

echo "verified packaged tray: pid=$verified_pid title=$item_name accessibility-label=$item_description frame=$item_x:$item_y:$item_width:$item_height left-click=toggle right-click=menu native-quit=passed cold-position=$cold_position"
