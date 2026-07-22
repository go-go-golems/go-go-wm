#!/bin/bash
# ggwm-xephyr-scenarios.sh — correctness sweep for the chrome-only upload.
#
# The drag harness only exercises tiled divider resizes. The chrome-only
# upload (GGWM-012 Step 12) assumes a reparented client covers exactly the
# frame interior, so any path that changes what covers a frame is a place that
# assumption could break: floats, fullscreen, workspace switches, focus
# changes, accept mode, and theme swaps.
#
# None of those would fail a unit test — a violation shows up as stale pixels.
# So this script drives each one and screenshots the result for inspection.
#
# Usage:  PARENT=:0 ./ggwm-xephyr-scenarios.sh [label]
set -uo pipefail

PARENT="${PARENT:-:0}"
NEST="${NEST:-:8}"
GEOM="${GEOM:-1280x800}"
LABEL="${1:-scenarios}"
OUT="${OUT:-$HOME/ggwm-xephyr}"
GO_GO_WM="${GO_GO_WM:-$HOME/ggwm-shm-ab/go-go-wm}"
SOCK="${XDG_RUNTIME_DIR:-/tmp}/go-go-wm-scen.sock"
PBUI_SOCK="${XDG_RUNTIME_DIR:-/tmp}/pbui-scen.sock"
SHOTS="${SHOTS:-$PWD/ttmp/2026/07/21/GGWM-012-GUIDES--import-go-go-wm-engineering-guides-and-handbook/images}"
mkdir -p "$OUT" "$SHOTS"
LOG="$OUT/$LABEL.jsonl"
exec > >(tee -a "$OUT/$LABEL.harness.log") 2>&1

XEPHYR_PID=""; WM_PID=""
cleanup() {
  echo "--- cleanup ---"
  [ -n "$WM_PID" ] && kill "$WM_PID" 2>/dev/null
  sleep 0.5
  [ -n "$XEPHYR_PID" ] && kill "$XEPHYR_PID" 2>/dev/null
  rm -f "$SOCK" "$PBUI_SOCK"
}
trap cleanup EXIT

ipc() {
  python3 - "$SOCK" "$1" <<'PY'
import socket,sys
s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
s.settimeout(5)
try:
    s.connect(sys.argv[1]); s.sendall((sys.argv[2]+"\n").encode())
    print(s.makefile().readline().strip()[:200])
except Exception as e:
    print('{"ok":false,"error":"%s"}' % e)
PY
}
shot() { sleep 0.6; DISPLAY="$NEST" import -window root "$SHOTS/$LABEL-$1.png" 2>/dev/null && echo "   shot: $LABEL-$1.png"; }

echo "=== $(date -Is) scenarios on $NEST ==="
rm -f "/tmp/.X${NEST#:}-lock" "$SOCK" "$PBUI_SOCK" "$LOG"
DISPLAY="$PARENT" Xephyr "$NEST" -screen "$GEOM" -ac -noreset >"$OUT/$LABEL.xephyr.log" 2>&1 &
XEPHYR_PID=$!
for i in $(seq 1 60); do DISPLAY="$NEST" xdotool getdisplaygeometry >/dev/null 2>&1 && break; sleep 0.25; done
DISPLAY="$NEST" xdotool getdisplaygeometry >/dev/null 2>&1 || { echo "!! no Xephyr"; exit 1; }

"$GO_GO_WM" wm --display "$NEST" --embedded-broker \
  --ipc-socket "$SOCK" --socket "$PBUI_SOCK" \
  --log-level debug --log-format json --log-file "$LOG" &
WM_PID=$!
for i in $(seq 1 80); do
  [ -S "$SOCK" ] && break
  kill -0 "$WM_PID" 2>/dev/null || { echo "!! WM died"; tail -5 "$LOG"; exit 1; }
  sleep 0.1
done
echo "-- WM up"

leaf_of() {
  ipc '{"q":"tree"}' | python3 -c '
import sys,json
try: r=json.loads(sys.stdin.read())
except Exception: print(""); raise SystemExit
d=r.get("data") or {}
out=[]
def walk(n):
    if not isinstance(n,dict): return
    if n.get("kind")=="leaf": out.append(n.get("id")); return
    for k in ("a","b"): walk(n.get(k))
cur=d.get("current")
for w in (d.get("workspaces") or []):
    if not cur or w.get("id")==cur: walk(w.get("root")); break
print(" ".join(x for x in out if x))
'
}

# --- 1. two tiled clients -----------------------------------------------
DISPLAY="$NEST" xterm -geometry 20x5 & sleep 1.5
first=$(leaf_of | awk '{print $1}')
[ -n "$first" ] && ipc "{\"q\":\"op\",\"op\":{\"op\":\"split-leaf\",\"node\":\"$first\",\"dir\":\"row\"}}" >/dev/null
DISPLAY="$NEST" xterm -geometry 20x5 & sleep 1.5
echo "-- scenario 1: two tiled clients"; shot 01-two-tiles

# --- 2. focus change (repaints two panes' chrome) -----------------------
leaves=$(leaf_of); a=$(echo "$leaves"|awk '{print $1}'); b=$(echo "$leaves"|awk '{print $2}')
echo "-- scenario 2: focus $b then $a"
[ -n "$b" ] && ipc "{\"q\":\"focus\",\"target\":\"$b\"}" >/dev/null
shot 02-focus-b
[ -n "$a" ] && ipc "{\"q\":\"focus\",\"target\":\"$a\"}" >/dev/null
shot 03-focus-a

# --- 3. fullscreen enter/exit -------------------------------------------
echo "-- scenario 3: fullscreen"
ipc '{"q":"fullscreen"}' >/dev/null; shot 04-fullscreen-on
ipc '{"q":"fullscreen"}' >/dev/null; shot 05-fullscreen-off

# --- 4. float lift / sink ------------------------------------------------
echo "-- scenario 4: float"
ipc '{"q":"float"}' >/dev/null; shot 06-float-on
ipc '{"q":"float"}' >/dev/null; shot 07-float-off

# --- 5. workspace switch away and back ----------------------------------
echo "-- scenario 5: workspace switch"
ipc '{"q":"op","op":{"op":"add-workspace","app":""}}' >/dev/null
ipc '{"q":"op","op":{"op":"switch-workspace","workspace":"ws-2"}}' >/dev/null
shot 08-workspace-2
ipc '{"q":"op","op":{"op":"switch-workspace","workspace":"ws-1"}}' >/dev/null
shot 09-workspace-1-back

# --- 6. theme swap (invalidates every cached surface) -------------------
echo "-- scenario 6: theme swap"
ipc '{"q":"set-theme","theme":"dark"}' >/dev/null; shot 10-theme-dark
ipc '{"q":"set-theme","theme":"paper"}' >/dev/null; shot 11-theme-paper

echo "-- perf:"; ipc '{"q":"perf"}' | head -c 600; echo
echo "-- screenshots in $SHOTS"
