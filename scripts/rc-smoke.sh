#!/usr/bin/env bash
# rc-smoke: end-to-end test of the in-process rc.js runtime (GGWM-002 P3).
#
# Boots Xvfb + the WM with examples/scripts/rc.js, then proves the A1
# attachment point works: the rc-registered verb is visible on the broker,
# and the rc-bound key (Mod4-e) really splits the tree when xdotool
# presses it. Exits non-zero on any failed assertion.
#
# Usage: scripts/rc-smoke.sh [display-number]   (default :79)
set -euo pipefail

DPY_NUM="${1:-79}"
DPY=":$DPY_NUM"
BIN="${GO_GO_WM_BIN:-$(mktemp -d)/go-go-wm}"
PBUI_SOCK="/tmp/ggwm-rcsmoke-$DPY_NUM-pbui.sock"
WM_SOCK="/tmp/ggwm-rcsmoke-$DPY_NUM-wm.sock"
LOG="$(mktemp -d)"
cd "$(dirname "$0")/.."

cleanup() {
  [ -n "${WM_PID:-}" ] && kill "$WM_PID" 2>/dev/null || true
  [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null || true
}
trap cleanup EXIT

[ -x "$BIN" ] || go build -o "$BIN" ./cmd/go-go-wm

rm -f "$PBUI_SOCK" "$WM_SOCK"
Xvfb "$DPY" -screen 0 1024x768x24 >"$LOG/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 1

# --spawn records the spawned child's environment: proves the WM
# injects PBUI_SOCKET/GO_GO_WM_SOCKET (the custom sockets, not the
# defaults) into everything it launches — so in-session tools and
# clicked pbui:// links reach THIS desktop.
CHILD_ENV="$LOG/child-env.txt"
"$BIN" wm --display "$DPY" --embedded-broker \
  --socket "$PBUI_SOCK" --ipc-socket "$WM_SOCK" \
  --spawn "env > $CHILD_ENV" \
  --rc examples/scripts/rc.js >"$LOG/wm.log" 2>&1 &
WM_PID=$!

for _ in $(seq 40); do [ -S "$WM_SOCK" ] && break; sleep 0.25; done
[ -S "$WM_SOCK" ] || { echo "FAIL: WM socket never appeared"; cat "$LOG/wm.log"; exit 1; }
sleep 1.5   # give rc.js time to load

leaves() {
  "$BIN" query tree --wm-socket "$WM_SOCK" --output json 2>/dev/null \
    | grep -c '"kind": "leaf"'
}

# 1. rc.js registered its verb on the broker.
if ! "$BIN" query verbs --socket "$PBUI_SOCK" --ptype tile 2>/dev/null | grep -q "tile.note"; then
  echo "FAIL: tile.note verb not registered by rc.js"; cat "$LOG/wm.log"; exit 1
fi
echo "ok: rc.js verb registered"

# 2. The rc-bound key splits the tree. The very first synthetic
# keypress after Xvfb boot can be swallowed while the keymap settles
# (seen under load), so retry a few times — a real binding regression
# still fails after five presses.
BEFORE="$(leaves)"
AFTER="$BEFORE"
for _ in 1 2 3 4 5; do
  DISPLAY="$DPY" xdotool key super+e
  sleep 1
  AFTER="$(leaves)"
  [ "$AFTER" -gt "$BEFORE" ] && break
done
if [ "$AFTER" -le "$BEFORE" ]; then
  echo "FAIL: Mod4-e did not split (leaves $BEFORE → $AFTER)"; cat "$LOG/wm.log"; exit 1
fi
echo "ok: Mod4-e split the tree ($BEFORE → $AFTER leaves)"

# 3. Mod4-Return spawns the --spawn command; assert the child inherited
# this session's sockets (not the default paths).
for _ in 1 2 3 4 5; do
  DISPLAY="$DPY" xdotool key super+Return
  sleep 0.6
  [ -f "$CHILD_ENV" ] && break
done
[ -f "$CHILD_ENV" ] || { echo "FAIL: spawned child never recorded its env"; cat "$LOG/wm.log"; exit 1; }
if ! grep -q "^PBUI_SOCKET=$PBUI_SOCK$" "$CHILD_ENV"; then
  echo "FAIL: child PBUI_SOCKET not the session socket:"; grep -E 'PBUI_SOCKET|GO_GO_WM_SOCKET' "$CHILD_ENV"; exit 1
fi
if ! grep -q "^GO_GO_WM_SOCKET=$WM_SOCK$" "$CHILD_ENV"; then
  echo "FAIL: child GO_GO_WM_SOCKET not the session socket:"; grep -E 'PBUI_SOCKET|GO_GO_WM_SOCKET' "$CHILD_ENV"; exit 1
fi
echo "ok: spawned children inherit the session sockets"

echo "rc-smoke: PASS"
