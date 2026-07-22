#!/bin/bash
# ggwm-xephyr-validate.sh — runtime validation of the GGWM-012 Phase 1 changes.
#
# Runs go-go-wm inside a nested Xephyr server on the caller's display, drives a
# scripted divider drag with xdotool, and dumps the perf counters. Unlike the
# VT harness this needs no console access and cannot take down a real session:
# the nested server is disposable and nothing here is xinit's client.
#
# Usage:  PARENT=:0 ./ggwm-xephyr-validate.sh [condition-label]
#
# What it is checking:
#   divider_paint_skipped >> divider_painted   the divider paint guard works
#   map_requests_skipped  >  0                 the map-state mirror works
#   motion_coalesced      >  0                 admission control works
#   ximg_creates          ~= frames_resized    per-tick surface recreation
#   layout_calls per tick ~= 1                 the split-rect cache works
set -uo pipefail


PARENT="${PARENT:-:0}"
NEST="${NEST:-:7}"
GEOM="${GEOM:-1280x800}"
LABEL="${1:-run}"
OUT="${OUT:-$HOME/ggwm-xephyr}"
GO_GO_WM="${GO_GO_WM:-$HOME/ggwm-shm-ab/go-go-wm}"
SOCK="${XDG_RUNTIME_DIR:-/tmp}/go-go-wm-xephyr.sock"
PBUI_SOCK="${XDG_RUNTIME_DIR:-/tmp}/pbui-xephyr.sock"
mkdir -p "$OUT"
LOG="$OUT/$LABEL.jsonl"
: > "$OUT/$LABEL.harness.log"
exec > >(tee -a "$OUT/$LABEL.harness.log") 2>&1

XEPHYR_PID=""; WM_PID=""
cleanup() {
  local rc=$?
  echo "--- cleanup (rc=$rc) ---"
  [ -n "$WM_PID" ] && kill "$WM_PID" 2>/dev/null
  sleep 0.5
  [ -n "$XEPHYR_PID" ] && kill "$XEPHYR_PID" 2>/dev/null
  rm -f "$SOCK" "$PBUI_SOCK"
  echo "--- done ---"
}
trap cleanup EXIT

ipc() {
  python3 - "$SOCK" "$1" <<'PY'
import socket,sys
s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
s.settimeout(5)
try:
    s.connect(sys.argv[1])
    s.sendall((sys.argv[2]+"\n").encode())
    print(s.makefile().readline().strip())
except Exception as e:
    print('{"ok":false,"error":"%s"}' % e)
PY
}

echo "=== $(date -Is)  parent=$PARENT nest=$NEST geom=$GEOM label=$LABEL ==="

# 1. Nested server -----------------------------------------------------------
rm -f "/tmp/.X${NEST#:}-lock"
DISPLAY="$PARENT" Xephyr "$NEST" -screen "$GEOM" -ac -noreset >"$OUT/$LABEL.xephyr.log" 2>&1 &
XEPHYR_PID=$!
for i in $(seq 1 60); do
  if DISPLAY="$NEST" xdotool getdisplaygeometry >/dev/null 2>&1; then break; fi
  sleep 0.25
done
if ! DISPLAY="$NEST" xdotool getdisplaygeometry >/dev/null 2>&1; then
  echo "!! Xephyr never came up; see $OUT/$LABEL.xephyr.log"; exit 1
fi
echo "-- Xephyr up on $NEST ($(DISPLAY=$NEST xdotool getdisplaygeometry))"

# 2. Window manager ----------------------------------------------------------
rm -f "$SOCK" "$PBUI_SOCK" "$LOG"
GO_GO_WM_SOCKET="$SOCK" PBUI_SOCKET="$PBUI_SOCK" \
  "$GO_GO_WM" wm --display "$NEST" --embedded-broker \
    --ipc-socket "$SOCK" --socket "$PBUI_SOCK" \
    --log-level debug --log-format json --log-file "$LOG" &
WM_PID=$!

for i in $(seq 1 80); do
  [ -S "$SOCK" ] && break
  if ! kill -0 "$WM_PID" 2>/dev/null; then
    echo "!! WM exited during startup; last log lines:"; tail -5 "$LOG"; exit 1
  fi
  sleep 0.1
done
if [ ! -S "$SOCK" ]; then echo "!! IPC socket never appeared"; tail -5 "$LOG"; exit 1; fi
echo "-- WM up (pid $WM_PID), socket $SOCK"

# 3. Two panes ---------------------------------------------------------------
DISPLAY="$NEST" xterm -geometry 20x5 & sleep 1.5
leaf=$(ipc '{"q":"tree"}' | python3 -c '
import sys,json
try: r=json.loads(sys.stdin.read())
except Exception: print(""); raise SystemExit
d=r.get("data") or {}
def walk(n):
    if not isinstance(n,dict): return None
    if n.get("kind")=="leaf": return n.get("id")
    for k in ("a","b"):
        v=walk(n.get(k))
        if v: return v
    return None
cur=d.get("current")
for w in (d.get("workspaces") or []):
    if not cur or w.get("id")==cur:
        print(walk(w.get("root")) or ""); break
')
echo "-- first leaf: ${leaf:-<none>}"
if [ -n "$leaf" ]; then
  echo "-- split: $(ipc "{\"q\":\"op\",\"op\":{\"op\":\"split-leaf\",\"node\":\"$leaf\",\"dir\":\"row\"}}")"
  DISPLAY="$NEST" xterm -geometry 20x5 & sleep 1.5
fi

# 3b. Screenshots -------------------------------------------------------------
# Saved into the ticket so the report can show what the WM actually looked
# like at each stage, and so a rendering regression is visible rather than
# inferred from counters.
SHOTS="${SHOTS:-$PWD/ttmp/2026/07/21/GGWM-012-GUIDES--import-go-go-wm-engineering-guides-and-handbook/images}"
mkdir -p "$SHOTS"
shot() { DISPLAY="$NEST" import -window root "$SHOTS/$LABEL-$1.png" 2>/dev/null && echo "-- shot: $LABEL-$1.png"; }
shot 1-before-drag

# 4. Baseline counters, then a scripted drag ---------------------------------
echo "-- reset: $(ipc '{"q":"perf-reset"}')"
W=${GEOM%x*}; H=${GEOM#*x}
DIVX=$((W/2)); DIVY=$((H/2))
LO=$((W/4)); HI=$((W*3/4))
echo "-- drag: x $LO..$HI at y=$DIVY (divider expected near $DIVX)"
export DISPLAY="$NEST"
xdotool mousemove $DIVX $DIVY; sleep 0.3
xdotool mousedown 1
for r in 1 2 3; do
  for x in $(seq $LO 6 $HI); do xdotool mousemove $x $DIVY; sleep 0.004; done
  for x in $(seq $HI -6 $LO); do xdotool mousemove $x $DIVY; sleep 0.004; done
done
shot 2-mid-drag
xdotool mousemove $DIVX $DIVY
xdotool mouseup 1
sleep 1
unset DISPLAY
shot 3-after-release

# 5. Results -----------------------------------------------------------------
ipc '{"q":"perf"}' > "$OUT/$LABEL.perf.json"
echo "-- perf counters:"
python3 -c '
import json,sys
r=json.load(open(sys.argv[1]))
d=r.get("data") or r
for k,v in d.items(): print(f"   {k:26s} {v}")
' "$OUT/$LABEL.perf.json" 2>/dev/null || cat "$OUT/$LABEL.perf.json"

echo "-- paintFrame samples: $(grep -c '"message":"paintFrame"' "$LOG" 2>/dev/null || echo 0)"
echo "-- log: $LOG"
