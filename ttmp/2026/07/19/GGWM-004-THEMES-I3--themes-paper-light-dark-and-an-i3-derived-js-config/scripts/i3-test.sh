#!/bin/bash
# GGWM-004 live verification: i3.js + themes on Xvfb :79.
set -u
REPO=/home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm
BIN=/tmp/claude-1000/go-go-wm
D=:79
RUN=/tmp/claude-1000/ggwm004
SHOTS="$RUN/shots"
mkdir -p "$RUN" "$SHOTS"
export GO_GO_WM_SOCKET="$RUN/wm.sock"
BROKER="$RUN/broker.sock"

cleanup() {
  pkill -f "Xvfb $D" 2>/dev/null
  pkill -f "go-go-wm wm --display $D" 2>/dev/null
  pkill -f "DISPLAY=$D" 2>/dev/null
}

ipc() { # ipc '{"q":"theme"}'
  python3 - "$1" <<'EOF'
import json, socket, sys
s = socket.socket(socket.AF_UNIX)
s.connect(__import__("os").environ["GO_GO_WM_SOCKET"])
s.sendall((sys.argv[1] + "\n").encode())
print(s.makefile().readline().strip())
EOF
}

shot() { DISPLAY=$D import -window root "$SHOTS/$1.png" 2>/dev/null || DISPLAY=$D xwd -root -silent | convert xwd:- "$SHOTS/$1.png"; }

case "${1:-run}" in
  stop) cleanup; exit 0 ;;
esac

cleanup; sleep 0.5
Xvfb $D -screen 0 1280x800x24 &
sleep 1
cd "$REPO"
$BIN wm --display $D --embedded-broker --socket "$BROKER" --ipc-socket "$GO_GO_WM_SOCKET" \
  --theme dark --no-default-binds --spawn xterm --rc examples/scripts/i3.js \
  > "$RUN/wm.log" 2>&1 &
sleep 2

echo "=== theme after boot (expect dark) ==="
ipc '{"q":"theme"}'
echo "=== tree: workspace names (expect 1..9) ==="
$BIN query tree --socket "$BROKER" 2>/dev/null | head -2 || true
python3 - <<'EOF'
import json, socket, os
s = socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
s.sendall(b'{"q":"tree"}\n')
d = json.loads(s.makefile().readline())["data"]
print("workspaces:", [w["name"] for w in d["workspaces"]], "current:", d["current"])
EOF

echo "=== spawn two xterms ==="
DISPLAY=$D xterm -T left  & sleep 0.7
DISPLAY=$D xterm -T right & sleep 0.7
ipc '{"q":"windows"}'

echo "=== focus left via IPC, then move right ==="
ipc '{"q":"focus","target":"left"}'
ipc '{"q":"move","dir":"right"}'

echo "=== keybinding: Mod4-2 (switch to ws 2) ==="
DISPLAY=$D xdotool key --clearmodifiers super+2
sleep 0.7
ipc '{"q":"tree"}' | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("current ws:", [w["name"] for w in d["workspaces"] if w["id"]==d["current"]])'

echo "=== keybinding: Mod4-Return spawns kitty on ws 2 ==="
DISPLAY=$D xdotool key --clearmodifiers super+Return
sleep 2.5
ipc '{"q":"windows"}'
shot 01-dark-ws2

echo "=== back_and_forth: Mod4-b returns to ws 1 ==="
DISPLAY=$D xdotool key --clearmodifiers super+b
sleep 0.7
ipc '{"q":"tree"}' | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("current ws:", [w["name"] for w in d["workspaces"] if w["id"]==d["current"]])'
shot 02-dark-ws1

echo "=== class rule: xterm with WM_CLASS Slack → ws 8 ==="
DISPLAY=$D xterm -class Slack -T slack-fake & sleep 1.2
ipc '{"q":"windows"}'

echo "=== live theme switches ==="
ipc '{"q":"set-theme","theme":"light"}'; sleep 0.8; shot 03-light
ipc '{"q":"set-theme","theme":"paper"}'; sleep 0.8; shot 04-paper
ipc '{"q":"set-theme","theme":"dark"}';  sleep 0.8; shot 05-dark
ipc '{"q":"theme"}'

echo "=== focus keys: Mod4-Left/Right on ws1 ==="
DISPLAY=$D xdotool key --clearmodifiers super+Left; sleep 0.4
ipc '{"q":"windows"}' | python3 -c 'import json,sys; [print("focused:", w["title"]) for w in json.load(sys.stdin)["data"] if w["focused"]]'
DISPLAY=$D xdotool key --clearmodifiers super+Right; sleep 0.4
ipc '{"q":"windows"}' | python3 -c 'import json,sys; [print("focused:", w["title"]) for w in json.load(sys.stdin)["data"] if w["focused"]]'

echo "=== wm log tail ==="
tail -5 "$RUN/wm.log"
echo "DONE (stack left running for screenshots; stop with: $0 stop)"
