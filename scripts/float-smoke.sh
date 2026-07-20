#!/usr/bin/env bash
# float-smoke: end-to-end test of the floating overlay layer (GGWM-007).
#
# Boots Xvfb + the WM, then proves each design property with the testwin
# client: dialogs float (tree untouched), floats die clean (no zombie),
# rules override detection in both directions, workspace switches keep
# the float's record, and the float toggle flips between the worlds.
#
# Usage: scripts/float-smoke.sh [display-number]   (default :86)
set -euo pipefail

DPY_NUM="${1:-86}"
DPY=":$DPY_NUM"
BIN="${GO_GO_WM_BIN:-$(mktemp -d)/go-go-wm}"
PBUI_SOCK="/tmp/ggwm-floatsmoke-$DPY_NUM-pbui.sock"
WM_SOCK="/tmp/ggwm-floatsmoke-$DPY_NUM-wm.sock"
LOG="$(mktemp -d)"
cd "$(dirname "$0")/.."

declare -a WIN_PIDS=()
cleanup() {
  for p in "${WIN_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  [ -n "${WM_PID:-}" ] && kill "$WM_PID" 2>/dev/null || true
  [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null || true
}
trap cleanup EXIT

[ -x "$BIN" ] || go build -o "$BIN" ./cmd/go-go-wm

rm -f "$PBUI_SOCK" "$WM_SOCK"
Xvfb "$DPY" -screen 0 1280x800x24 >"$LOG/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 1

"$BIN" wm --display "$DPY" --embedded-broker \
  --socket "$PBUI_SOCK" --ipc-socket "$WM_SOCK" >"$LOG/wm.log" 2>&1 &
WM_PID=$!
for _ in $(seq 40); do [ -S "$WM_SOCK" ] && break; sleep 0.25; done
[ -S "$WM_SOCK" ] || { echo "FAIL: WM socket never appeared"; cat "$LOG/wm.log"; exit 1; }
sleep 0.5

# One NDJSON request/response over the control socket.
ipc() {
  python3 - "$WM_SOCK" "$1" <<'PYEOF'
import socket, sys
s = socket.socket(socket.AF_UNIX)
s.settimeout(5)
s.connect(sys.argv[1])
s.sendall((sys.argv[2] + "\n").encode())
print(s.makefile().readline().strip())
PYEOF
}
windows() { ipc '{"q":"windows"}'; }
leaves() { ipc '{"q":"tree"}' | grep -o '"kind":"leaf"' | wc -l; }
float_count() { windows | grep -o '"floating":true' | wc -l; }
# floating_of <title>: prints true/false/absent for the row with title.
floating_of() {
  windows | python3 -c '
import json, sys
rows = json.loads(sys.stdin.read()).get("data") or []
m = [r for r in rows if r.get("title") == sys.argv[1]]
print("absent" if not m else ("true" if m[0].get("floating") else "false"))
' "$1"
}
# wait_until <iters> <fn> <args...>
wait_until() {
  local n="$1"; shift
  for _ in $(seq "$n"); do "$@" && return 0; sleep 0.25; done
  return 1
}
float_count_is() { [ "$(float_count)" -eq "$1" ]; }
title_is_floating() { [ "$(floating_of "$1")" = "true" ]; }
title_is_tiled() { [ "$(floating_of "$1")" = "false" ]; }

fail() {
  echo "FAIL: $1"
  echo "--- windows:"; windows || true
  echo "--- wm.log tail:"; tail -20 "$LOG/wm.log"
  exit 1
}

# 1. A dialog-typed window floats and leaves the tree untouched.
BEFORE_LEAVES="$(leaves)"
"$BIN" testwin --display "$DPY" --type dialog --title saveas >"$LOG/w1.out" 2>&1 &
WIN_PIDS+=($!)
wait_until 20 title_is_floating saveas || fail "dialog testwin never floated"
[ "$(leaves)" -eq "$BEFORE_LEAVES" ] || fail "float changed the leaf count"
echo "ok: dialog floats, tree untouched ($BEFORE_LEAVES leaves)"

# 2. Fixed-size window (min==max hints) floats.
"$BIN" testwin --display "$DPY" --fixed 300x120 --title fixedwin >"$LOG/w2.out" 2>&1 &
WIN_PIDS+=($!)
wait_until 20 title_is_floating fixedwin || fail "fixed-size testwin never floated"
echo "ok: fixed-size window floats"

# 3. Kill a float client → record disappears, no zombie frame.
kill "${WIN_PIDS[1]}"
wait_until 20 float_count_is 1 || fail "killed float still present (zombie)"
echo "ok: float teardown clean"

# 4. Rules override detection both ways: float:false tiles a dialog,
# float:true floats a plain window.
ipc '{"q":"set-float-rules","float_rules":[{"class":"Forcetile","float":false},{"class":"Forcefloat","float":true}]}' \
  | grep -q '"ok":true' || fail "set-float-rules rejected"
"$BIN" testwin --display "$DPY" --type dialog --class Forcetile --title forcedtile >"$LOG/w3.out" 2>&1 &
WIN_PIDS+=($!)
wait_until 20 title_is_tiled forcedtile || fail "float:false rule did not force tiling"
"$BIN" testwin --display "$DPY" --class Forcefloat --title forcedfloat >"$LOG/w4.out" 2>&1 &
WIN_PIDS+=($!)
wait_until 20 title_is_floating forcedfloat || fail "float:true rule did not force floating"
echo "ok: rules override detection both ways"

# 5. Workspace association: the float keeps its record across a
# workspace round trip (hidden there, reshown here).
FIRST_WS="$(ipc '{"q":"tree"}' | python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["data"]["workspaces"][0]["id"])')"
ipc '{"q":"op","op":{"op":"add-workspace"}}' >/dev/null
sleep 0.5
title_is_floating forcedfloat || fail "float lost its record on workspace switch"
ipc "{\"q\":\"op\",\"op\":{\"op\":\"switch-workspace\",\"workspace\":\"$FIRST_WS\"}}" >/dev/null
sleep 0.5
title_is_floating forcedfloat || fail "float lost after switching back"
echo "ok: float survives workspace round trip"

# 6. The toggle flips between worlds: two toggles yield opposite
# states (which starts depends on where focus sits).
R1="$(ipc '{"q":"float"}')"
echo "$R1" | grep -q '"ok":true' || fail "first toggle rejected: $R1"
sleep 0.5
R2="$(ipc '{"q":"float"}')"
echo "$R2" | grep -q '"ok":true' || fail "second toggle rejected: $R2"
D1="$(echo "$R1" | grep -o '"data":[a-z]*')"
D2="$(echo "$R2" | grep -o '"data":[a-z]*')"
[ "$D1" != "$D2" ] || fail "toggles did not alternate ($D1 then $D2)"
echo "ok: float toggle alternates ($D1 → $D2)"

# 7. Fullscreen toggle (GGWM-007 follow-up): the focused window covers
# the whole screen (bars included), and toggling back restores the
# tiled rect.
ipc '{"q":"focus","target":"next"}' >/dev/null
ipc '{"q":"fullscreen"}' | grep -q '"data":true' || fail "fullscreen toggle rejected"
sleep 0.5
windows | python3 -c '
import json, sys
rows = json.loads(sys.stdin.read())["data"]
fs = [r for r in rows if r["rect"] == "1280x800+0+0"]
assert fs, "no window at full screen size: %r" % [r["rect"] for r in rows]
' || fail "fullscreen window does not cover the screen"
ipc '{"q":"fullscreen"}' | grep -q '"data":false' || fail "fullscreen exit rejected"
sleep 0.5
windows | python3 -c '
import json, sys
rows = json.loads(sys.stdin.read())["data"]
assert not [r for r in rows if r["rect"] == "1280x800+0+0"], "still fullscreen"
' || fail "exit did not restore tiled geometry"
echo "ok: fullscreen covers the screen and restores"

# 8. Theme switching repaints WM chrome. The bottom status bar is an
# Ink-background band: dark in paper/light themes, LIGHT in dark theme.
# Sample a bottom-bar pixel across a paper→dark switch and require it
# to flip — proving the bar (an XSurfaceSet-pixmap window) actually
# repaints rather than showing a detached back pixel (the GGWM-004/006
# CwBackPixel-detaches-the-pixmap trap).
barpx() { # → r+g+b brightness of a bottom-bar pixel
  DISPLAY="$DPY" import -window root -crop 1x1+30+796 -depth 8 txt:- 2>/dev/null \
    | grep -o '([0-9]*,[0-9]*,[0-9]*' | head -1 | tr -d '(' \
    | awk -F, '{print $1+$2+$3}'
}
B_PAPER="$(barpx)"
ipc '{"q":"set-theme","theme":"dark"}' >/dev/null
sleep 1
B_DARK="$(barpx)"
[ "$B_PAPER" -lt 200 ] || fail "paper bottom bar not dark (got $B_PAPER) — did it repaint?"
[ "$B_DARK" -gt 500 ] || fail "dark-theme bottom bar not light (got $B_DARK) — chrome did not repaint"
ipc '{"q":"set-theme","theme":"paper"}' >/dev/null
echo "ok: theme switch repaints chrome (bottom bar $B_PAPER -> $B_DARK)"

echo "PASS: float smoke"
