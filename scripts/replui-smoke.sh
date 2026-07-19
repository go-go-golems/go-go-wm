#!/usr/bin/env bash
# replui-smoke: end-to-end test of the rich REPL surface (GGWM-009).
#
# Boots Xvfb + WM + `repl --ui`, types cells through the managed window,
# and asserts through repl.cell-done bus events: numbers, colors,
# datasets, console capture, errors, and Out(n) history. The accept
# stage is the ticket's thesis: a color Out[k] answers a desktop
# accept — the swatch is located by scanning a screenshot for its exact
# RGB and clicked.
#
# Usage: scripts/replui-smoke.sh [display-number]   (default :88)
set -euo pipefail

DPY_NUM="${1:-88}"
DPY=":$DPY_NUM"
BIN="${GO_GO_WM_BIN:-$(mktemp -d)/go-go-wm}"
PBUI_SOCK="/tmp/ggwm-replsmoke-$DPY_NUM-pbui.sock"
WM_SOCK="/tmp/ggwm-replsmoke-$DPY_NUM-wm.sock"
LOG="$(mktemp -d)"
cd "$(dirname "$0")/.."

cleanup() {
  [ -n "${EVENTS_PID:-}" ] && kill "$EVENTS_PID" 2>/dev/null || true
  [ -n "${REPL_PID:-}" ] && kill "$REPL_PID" 2>/dev/null || true
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

DISPLAY="$DPY" "$BIN" repl --ui --socket "$PBUI_SOCK" --wm-socket "$WM_SOCK" \
  >"$LOG/repl.log" 2>&1 &
REPL_PID=$!

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
wait_until() {
  local n="$1"; shift
  for _ in $(seq "$n"); do "$@" >/dev/null 2>&1 && return 0; sleep 0.25; done
  return 1
}
repl_managed() { ipc '{"q":"windows"}' | grep -q '"title":"repl"'; }

# expect_event <pattern...> -- <action...>: start a bus follower, run
# the action, wait out the follower's timeout (glazed flushes rows on
# exit — including SIGTERM/timeout exit), then grep. All patterns must
# match the captured stream.
expect_event() {
  local patterns=()
  while [ "$1" != "--" ]; do patterns+=("$1"); shift; done
  shift
  rm -f "$LOG/ev.out"
  timeout 8 "$BIN" query events --socket "$PBUI_SOCK" --count 999 \
    --output json --output-as-objects >"$LOG/ev.out" 2>&1 &
  local EP=$!
  sleep 0.8
  "$@"
  wait "$EP" 2>/dev/null || true
  for pat in "${patterns[@]}"; do
    grep -q "$pat" "$LOG/ev.out" || return 1
  done
}

fail() {
  echo "FAIL: $1"
  echo "--- last event capture:"; grep -A1 repl.cell-done "$LOG/ev.out" 2>/dev/null | tail -8
  echo "--- repl.log:"; tail -10 "$LOG/repl.log"
  echo "--- wm.log:"; tail -5 "$LOG/wm.log"
  exit 1
}

wait_until 60 repl_managed || fail "repl window never managed"
sleep 1
echo "ok: repl --ui window managed"

# The repl tile has input focus (the WM focuses new clients).
type_line() {
  DISPLAY="$DPY" xdotool type --delay 30 "$1"
  DISPLAY="$DPY" xdotool key Return
}

# 1. Numbers evaluate.
expect_event 'ptype.....number' 'summary.....2' -- type_line "1+1" \
  || fail "cell 1 (1+1) never completed as number 2"
echo "ok: numbers evaluate (Out[1] = 2)"

# 2. A color literal derives ptype color.
expect_event 'ptype.....color' -- type_line '"#aa5533"' \
  || fail "cell 2 never derived color"
echo "ok: color literal derives a color presentation"

# 3. THE thesis: Out[2] answers a desktop accept. Find the swatch by
# its exact RGB in a screenshot and click it.
"$BIN" accept --ptype color --socket "$PBUI_SOCK" >"$LOG/accept.out" 2>&1 &
ACCEPT_PID=$!
sleep 1
COORDS="$(DISPLAY="$DPY" import -window root -depth 8 txt:- 2>/dev/null \
  | grep -im1 'AA5533' | cut -d: -f1 || true)"
[ -n "$COORDS" ] || fail "swatch pixel #aa5533 not found on screen"
X="${COORDS%,*}"; Y="${COORDS#*,}"
DISPLAY="$DPY" xdotool mousemove "$X" "$Y" click 1
AOK=0
for _ in $(seq 20); do
  grep -q "aa5533" "$LOG/accept.out" 2>/dev/null && AOK=1 && break
  sleep 0.25
done
wait "$ACCEPT_PID" 2>/dev/null || true
[ "$AOK" = 1 ] || fail "clicking the Out[2] swatch did not answer the accept: $(cat "$LOG/accept.out")"
echo "ok: Out[2] answered accept(color) by click at $X,$Y"

# 4. Datasets derive with a table view.
expect_event 'ptype.....dataset' '2 rows' -- type_line "[{a:1,b:2},{a:3,b:4}]" \
  || fail "cell 3 never derived a dataset"
echo "ok: datasets derive (2 rows x 2 cols)"

# 5. Console capture + statement fallback (the wrap parse-fails, the
# verbatim path runs).
expect_event 'console...1' -- type_line 'console.log("hello"); 5' \
  || fail "cell 4 console not captured"
echo "ok: console capture on the statement path"

# 6. Errors surface.
expect_event 'SyntaxError' -- type_line "nope(" \
  || fail "cell 5 error not surfaced"
echo "ok: errors surface in the cell"

# 7. Out history computes.
expect_event 'summary.....3' -- type_line "Out(1)+1" \
  || fail "Out(1)+1 did not compute 3"
echo "ok: Out(n) history is live"

# 8. The modules are pre-bound: wm works without require().
expect_event 'summary.....3' -- type_line "wm.themes().length" \
  || fail "wm is not pre-bound in the notebook"
echo "ok: wm/pbui/ui are pre-bound"

# 9. The REPL's verbs are on the broker for its result ptypes.
"$BIN" query verbs --socket "$PBUI_SOCK" --ptype series 2>/dev/null | grep -q "repl.use" \
  || fail "repl.use verb not registered for series"
echo "ok: repl verbs registered desktop-wide"

# 10. Theme switching repaints the notebook (palette swap via the
# event fan + posted redraw). Sample an interior pixel: paper → dark.
sample() { # x y → "r,g,b"
  DISPLAY="$DPY" import -window root -crop 1x1+"$1"+"$2" -depth 8 txt:- 2>/dev/null \
    | grep -o '([0-9]*,[0-9]*,[0-9]*' | head -1 | tr -d '('
}
brightness() { echo "$1" | awk -F, '{print $1+$2+$3}'; }
PX=200; PY=400   # inside the repl tile's content area
BEFORE="$(sample $PX $PY)"
[ "$(brightness "$BEFORE")" -gt 450 ] || fail "pre-switch pixel not light: $BEFORE"
ipc '{"q":"set-theme","theme":"dark"}' | grep -q '"ok":true' || fail "set-theme rejected"
sleep 2
AFTER="$(sample $PX $PY)"
[ "$(brightness "$AFTER")" -lt 250 ] || fail "repl window did not go dark: $BEFORE -> $AFTER"
ipc '{"q":"set-theme","theme":"paper"}' >/dev/null
echo "ok: theme switch repaints the notebook ($BEFORE -> $AFTER)"

echo "PASS: replui smoke"
