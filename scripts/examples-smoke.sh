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
  [ -n "${NETVERBS_PID:-}" ] && kill "$NETVERBS_PID" 2>/dev/null || true
  [ -n "${JSCOLORS_PID:-}" ] && kill "$JSCOLORS_PID" 2>/dev/null || true
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

"$BIN" run --socket "$PBUI_SOCK" examples/scripts/net-verbs.js >"$LOG/netverbs.log" 2>&1 &
NETVERBS_PID=$!
sleep 1
"$BIN" query verbs --socket "$PBUI_SOCK" --ptype ip | grep -q "ip.octets" \
  || { echo "FAIL: net-verbs.js ip verbs missing"; exit 1; }
"$BIN" query verbs --socket "$PBUI_SOCK" --ptype url | grep -q "url.host" \
  || { echo "FAIL: net-verbs.js url verbs missing"; exit 1; }
echo "ok: net-verbs.js registered ip + url verbs"

( sleep 1; "$BIN" answer --socket "$PBUI_SOCK" --ptype color --value '#5a7a58' ) &
"$BIN" run --once --socket "$PBUI_SOCK" examples/scripts/palette.js
echo "ok: palette.js accept flow"

kill "$GITVERBS_PID" "${NETVERBS_PID:-}" 2>/dev/null || true
kill "$BROKER_PID" 2>/dev/null || true
unset GITVERBS_PID NETVERBS_PID BROKER_PID

# ---- stage 2: real WM in Xvfb -------------------------------------------
rm -f "$PBUI_SOCK" "$WM_SOCK"
Xvfb "$DPY" -screen 0 1024x768x24 >"$LOG/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 1
"$BIN" wm --display "$DPY" --embedded-broker \
  --socket "$PBUI_SOCK" --ipc-socket "$WM_SOCK" \
  --rc examples/scripts/rc-tile.js >"$LOG/wm.log" 2>&1 &
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

# rc-tile.js ran as the WM's rc: the scripted tile must be in the tree.
"$BIN" query tree --wm-socket "$WM_SOCK" | grep -q "script:js-counter" \
  || { echo "FAIL: rc-tile.js script tile missing"; cat "$LOG/wm.log"; exit 1; }
echo "ok: rc-tile.js registered and placed its tile"

# js-colors.js: a JS ui.app serving verbs on the desktop.
DISPLAY="$DPY" "$BIN" run --socket "$PBUI_SOCK" --wm-socket "$WM_SOCK" \
  examples/scripts/js-colors.js >"$LOG/jscolors.log" 2>&1 &
JSCOLORS_PID=$!
sleep 2
"$BIN" query verbs --socket "$PBUI_SOCK" --ptype color | grep -q "color.darken" \
  || { echo "FAIL: js-colors.js verb missing"; cat "$LOG/jscolors.log"; exit 1; }
kill "$JSCOLORS_PID" 2>/dev/null || true
echo "ok: js-colors.js ui.app is a desktop citizen"

# ---- stage 3: i3.js as the whole config (GGWM-004) -----------------------
kill "$WM_PID" 2>/dev/null || true
wait "$WM_PID" 2>/dev/null || true
rm -f "$PBUI_SOCK" "$WM_SOCK"
"$BIN" wm --display "$DPY" --embedded-broker \
  --socket "$PBUI_SOCK" --ipc-socket "$WM_SOCK" \
  --theme dark --no-default-binds --rc examples/scripts/i3.js >"$LOG/wm-i3.log" 2>&1 &
WM_PID=$!
for _ in $(seq 40); do [ -S "$WM_SOCK" ] && break; sleep 0.25; done
[ -S "$WM_SOCK" ] || { echo "FAIL: i3.js WM never came up"; cat "$LOG/wm-i3.log"; exit 1; }

python3 - "$WM_SOCK" <<'PYEOF'
import json, socket, sys, time
def q(req):
    s = socket.socket(socket.AF_UNIX); s.connect(sys.argv[1])
    s.sendall((json.dumps(req) + "\n").encode())
    r = json.loads(s.makefile().readline())
    assert r["ok"], r
    return r["data"]
# rc.js pre-creates workspaces 1..9 at boot; poll rather than racing it.
deadline = time.time() + 20
names = []
while time.time() < deadline:
    d = q({"q": "tree"})
    names = [w["name"] for w in d["workspaces"]]
    if names == [str(n) for n in range(1, 10)]:
        break
    time.sleep(0.5)
assert names == [str(n) for n in range(1, 10)], names
while time.time() < deadline:
    d = q({"q": "tree"})
    if d["current"] == d["workspaces"][0]["id"]:
        break
    time.sleep(0.5)
assert d["current"] == d["workspaces"][0]["id"], (d["current"], names)
print("ok: i3.js pre-created workspaces 1..9, home is 1")
info = q({"q": "theme"})
assert info["theme"] == "dark", info
q({"q": "set-theme", "theme": "light"})
assert q({"q": "theme"})["theme"] == "light"
q({"q": "set-theme", "theme": "dark"})
print("ok: i3.js booted dark; set-theme round-trips")
PYEOF

echo "examples-smoke: PASS"
