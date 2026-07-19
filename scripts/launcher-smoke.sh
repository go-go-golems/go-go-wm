#!/usr/bin/env bash
# launcher-smoke: end-to-end test of the launcher popup (GGWM-008 L2).
#
# Boots Xvfb + the WM against a fixture XDG data dir, then proves:
# the popup opens with registry rows, typed keys filter (input focus on
# the popup), Enter launches the selection (a marker .desktop entry),
# Mod4+d toggles, Escape closes, and a builtin entry lands in the tree.
#
# Usage: scripts/launcher-smoke.sh [display-number]   (default :87)
set -euo pipefail

DPY_NUM="${1:-87}"
DPY=":$DPY_NUM"
BIN="${GO_GO_WM_BIN:-$(mktemp -d)/go-go-wm}"
PBUI_SOCK="/tmp/ggwm-launchersmoke-$DPY_NUM-pbui.sock"
WM_SOCK="/tmp/ggwm-launchersmoke-$DPY_NUM-wm.sock"
LOG="$(mktemp -d)"
FIXHOME="$(mktemp -d)"
MARKER="$LOG/launched.marker"
cd "$(dirname "$0")/.."

cleanup() {
  [ -n "${WM_PID:-}" ] && kill "$WM_PID" 2>/dev/null || true
  [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null || true
}
trap cleanup EXIT

[ -x "$BIN" ] || go build -o "$BIN" ./cmd/go-go-wm

# Fixture XDG data dir: one launchable marker entry + one decoy.
mkdir -p "$FIXHOME/share/applications"
cat > "$FIXHOME/share/applications/marker.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Marker App
Comment=touches a file so tests can see the launch
Exec=touch $MARKER
EOF
cat > "$FIXHOME/share/applications/decoy.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Zzz Decoy
Exec=false
EOF

rm -f "$PBUI_SOCK" "$WM_SOCK"
Xvfb "$DPY" -screen 0 1280x800x24 >"$LOG/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 1

HOME="$FIXHOME" XDG_DATA_DIRS="$FIXHOME/share" XDG_STATE_HOME="$FIXHOME/state" \
  "$BIN" wm --display "$DPY" --embedded-broker \
  --socket "$PBUI_SOCK" --ipc-socket "$WM_SOCK" >"$LOG/wm.log" 2>&1 &
WM_PID=$!
for _ in $(seq 40); do [ -S "$WM_SOCK" ] && break; sleep 0.25; done
[ -S "$WM_SOCK" ] || { echo "FAIL: WM socket never appeared"; cat "$LOG/wm.log"; exit 1; }
sleep 1 # first registry scan runs off-loop

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
linfo() { ipc '{"q":"launcher"}'; }
# lfield <python-expr over d (the data object)>
lfield() {
  linfo | python3 -c "
import json, sys
d = json.loads(sys.stdin.read())['data']
print($1)
"
}
wait_until() {
  local n="$1"; shift
  for _ in $(seq "$n"); do "$@" >/dev/null 2>&1 && return 0; sleep 0.25; done
  return 1
}
is_open() { [ "$(lfield "d['open']")" = "True" ]; }
is_closed() { [ "$(lfield "d['open']")" = "False" ]; }

fail() {
  echo "FAIL: $1"
  echo "--- launcher:"; linfo || true
  echo "--- wm.log tail:"; tail -20 "$LOG/wm.log"
  exit 1
}

# 1. Open via IPC: registry rows present (2 fixture apps + 4 builtins).
ipc '{"q":"launcher-open"}' | grep -q '"open":true' || fail "launcher-open rejected"
N="$(lfield "len(d['rows'])")"
[ "$N" -ge 6 ] || fail "expected >=6 rows, got $N"
echo "ok: popup opens with $N registry rows"

# 2. Typing filters (the popup holds input focus).
DISPLAY="$DPY" xdotool type --delay 40 "marker"
wait_until 20 bash -c "[ \"\$(printf '%s\n' '{\"q\":\"launcher\"}' | true)\" ] " || true
QOK=0
for _ in $(seq 20); do
  [ "$(lfield "d['query']")" = "marker" ] && QOK=1 && break
  sleep 0.25
done
[ "$QOK" = 1 ] || fail "typed query never arrived (query=$(lfield "d['query']"))"
[ "$(lfield "d['rows'][0]")" = "app:marker" ] || fail "filter did not rank app:marker first"
echo "ok: typed filter reaches the popup and ranks the marker app first"

# 3. Enter launches the selection and closes the popup.
DISPLAY="$DPY" xdotool key Return
wait_until 40 test -f "$MARKER" || fail "marker file never appeared (launch failed)"
wait_until 20 is_closed || fail "popup still open after launch"
echo "ok: Enter launches the selection (marker file exists)"

# 4. Mod4+d toggles (retry: first synthetic keypress may be swallowed).
OPENED=0
for _ in 1 2 3 4 5; do
  DISPLAY="$DPY" xdotool key super+d
  sleep 0.5
  is_open && OPENED=1 && break
done
[ "$OPENED" = 1 ] || fail "Mod4+d never opened the popup"
DISPLAY="$DPY" xdotool key super+d
wait_until 20 is_closed || fail "Mod4+d did not close the popup"
echo "ok: Mod4+d toggles"

# 5. Escape closes.
ipc '{"q":"launcher-open"}' >/dev/null
DISPLAY="$DPY" xdotool key Escape
wait_until 20 is_closed || fail "Escape did not close the popup"
echo "ok: Escape closes"

# 6. A builtin entry lands in the tree.
ipc '{"q":"launcher-open"}' >/dev/null
DISPLAY="$DPY" xdotool type --delay 40 "trace"
sleep 0.5
DISPLAY="$DPY" xdotool key Return
TOK=0
for _ in $(seq 20); do
  ipc '{"q":"tree"}' | grep -q '"app":"builtin:trace"' && TOK=1 && break
  sleep 0.25
done
[ "$TOK" = 1 ] || fail "builtin:trace never appeared in the tree"
echo "ok: builtin launch sets the leaf app"

# --- L3: the launcher tile (frame keyboard substrate) ----------------------

tfield() {
  ipc '{"q":"launcher-tile"}' | python3 -c "
import json, sys
d = json.loads(sys.stdin.read())['data']
print($1)
"
}

# 7. A fresh workspace focuses its empty launcher tile; typing filters it.
ipc '{"q":"op","op":{"op":"add-workspace"}}' >/dev/null
sleep 0.5
[ -n "$(tfield "d['leaf']")" ] || fail "no launcher tile focused after add-workspace"
DISPLAY="$DPY" xdotool type --delay 40 "trace"
TQ=0
for _ in $(seq 20); do
  [ "$(tfield "d['query']")" = "trace" ] && TQ=1 && break
  sleep 0.25
done
[ "$TQ" = 1 ] || fail "typed query never reached the tile (query=$(tfield "d['query']"))"
[ "$(tfield "d['rows'][0]")" = "builtin:trace" ] || fail "tile filter did not rank builtin:trace first"
echo "ok: typed keys reach the focused launcher tile"

# 8. WM chords still win while a tile is focused: Mod4+d opens the
# popup and the tile's query is untouched.
CHORD=0
for _ in 1 2 3 4 5; do
  DISPLAY="$DPY" xdotool key super+d
  sleep 0.5
  is_open && CHORD=1 && break
done
[ "$CHORD" = 1 ] || fail "Mod4+d chord swallowed by the tile"
ipc '{"q":"launcher-close"}' >/dev/null
[ "$(tfield "d['query']")" = "trace" ] || fail "chord leaked characters into the tile"
echo "ok: WM chords never reach the tile"

# 9. Enter launches into this tile (builtin takes the leaf over).
LEAF="$(tfield "d['leaf']")"
DISPLAY="$DPY" xdotool key Return
TOK=0
for _ in $(seq 20); do
  ipc '{"q":"tree"}' | python3 -c "
import json, sys
d = json.loads(sys.stdin.read())['data']
def walk(n):
    if n is None: return False
    if n.get('id') == '$LEAF': return n.get('app') == 'builtin:trace'
    return walk(n.get('a')) or walk(n.get('b'))
ok = any(walk(w['root']) for w in d['workspaces'])
raise SystemExit(0 if ok else 1)
" && TOK=1 && break
  sleep 0.25
done
[ "$TOK" = 1 ] || fail "Enter did not launch builtin:trace into leaf $LEAF"
[ -z "$(tfield "d['leaf']")" ] || fail "launcher tile state survived the launch"
echo "ok: Enter launches into the tile itself"

echo "PASS: launcher smoke"
