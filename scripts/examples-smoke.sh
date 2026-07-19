#!/usr/bin/env bash
# examples-smoke: every example script is a test fixture (GGWM-002 P4).
#
# Terminal-only examples run against a bare broker; layout examples run
# against a real WM in Xvfb. Everything asserts, everything exits
# non-zero on failure.
#
# Usage: scripts/examples-smoke.sh [display-number]   (default :80)
set -euo pipefail

DPY_NUM="${1:-80}"
DPY=":$DPY_NUM"
BIN="${GO_GO_WM_BIN:-$(mktemp -d)/go-go-wm}"
PBUI_SOCK="/tmp/ggwm-exsmoke-$DPY_NUM-pbui.sock"
WM_SOCK="/tmp/ggwm-exsmoke-$DPY_NUM-wm.sock"
LOG="$(mktemp -d)"
cd "$(dirname "$0")/.."

cleanup() {
  [ -n "${GITVERBS_PID:-}" ] && kill "$GITVERBS_PID" 2>/dev/null || true
  [ -n "${WM_PID:-}" ] && kill "$WM_PID" 2>/dev/null || true
  [ -n "${BROKER_PID:-}" ] && kill "$BROKER_PID" 2>/dev/null || true
  [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null || true
}
trap cleanup EXIT

[ -x "$BIN" ] || go build -o "$BIN" ./cmd/go-go-wm

# ---- stage 1: bare broker ------------------------------------------------
rm -f "$PBUI_SOCK"
"$BIN" broker --socket "$PBUI_SOCK" >"$LOG/broker.log" 2>&1 &
BROKER_PID=$!
for _ in $(seq 20); do [ -S "$PBUI_SOCK" ] && break; sleep 0.25; done

"$BIN" run --once --socket "$PBUI_SOCK" examples/scripts/hello.js
echo "ok: hello.js"

"$BIN" run --socket "$PBUI_SOCK" examples/scripts/git-verbs.js >"$LOG/gitverbs.log" 2>&1 &
GITVERBS_PID=$!
sleep 1
"$BIN" query verbs --socket "$PBUI_SOCK" --ptype git-commit | grep -q "git.compare-with" \
  || { echo "FAIL: git-verbs.js verbs missing"; exit 1; }
echo "ok: git-verbs.js registered its verbs"

( sleep 1; "$BIN" answer --socket "$PBUI_SOCK" --ptype color --value '#5a7a58' ) &
"$BIN" run --once --socket "$PBUI_SOCK" examples/scripts/palette.js
echo "ok: palette.js accept flow"

kill "$GITVERBS_PID" 2>/dev/null || true
kill "$BROKER_PID" 2>/dev/null || true
unset GITVERBS_PID BROKER_PID

# ---- stage 2: real WM in Xvfb -------------------------------------------
rm -f "$PBUI_SOCK" "$WM_SOCK"
Xvfb "$DPY" -screen 0 1024x768x24 >"$LOG/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 1
"$BIN" wm --display "$DPY" --embedded-broker \
  --socket "$PBUI_SOCK" --ipc-socket "$WM_SOCK" >"$LOG/wm.log" 2>&1 &
WM_PID=$!
for _ in $(seq 40); do [ -S "$WM_SOCK" ] && break; sleep 0.25; done
[ -S "$WM_SOCK" ] || { echo "FAIL: WM never came up"; cat "$LOG/wm.log"; exit 1; }
sleep 1

"$BIN" run --once --socket "$PBUI_SOCK" --wm-socket "$WM_SOCK" examples/scripts/golden.js
echo "ok: golden.js self-asserted"

"$BIN" run --once --socket "$PBUI_SOCK" --wm-socket "$WM_SOCK" examples/scripts/project-switcher.js
"$BIN" query tree --wm-socket "$WM_SOCK" | grep -q "go-go-wm" \
  || { echo "FAIL: project-switcher workspace missing"; exit 1; }
echo "ok: project-switcher.js built its workspace"

echo "examples-smoke: PASS"
