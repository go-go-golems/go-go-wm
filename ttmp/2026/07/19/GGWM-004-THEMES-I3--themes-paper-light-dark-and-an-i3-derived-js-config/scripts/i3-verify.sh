#!/bin/bash
set -u
export GO_GO_WM_SOCKET=/tmp/claude-1000/ggwm004/wm.sock
D=:79
ipc() {
  python3 - "$1" <<'EOF'
import json, socket, sys, os
s = socket.socket(socket.AF_UNIX)
s.connect(os.environ["GO_GO_WM_SOCKET"])
s.sendall((sys.argv[1] + "\n").encode())
print(s.makefile().readline().strip())
EOF
}
cur() { ipc '{"q":"tree"}' | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("current:", [w["name"] for w in d["workspaces"] if w["id"]==d["current"]][0])'; }
foc() { ipc '{"q":"windows"}' | python3 -c 'import json,sys; [print("focused:", w["title"], w.get("class","")) for w in json.load(sys.stdin)["data"] if w["focused"]]'; }

echo "--- theme ---"; ipc '{"q":"theme"}'
echo "--- workspaces ---"
ipc '{"q":"tree"}' | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print([w["name"] for w in d["workspaces"]])'
cur
echo "--- windows (class column) ---"
ipc '{"q":"windows"}' | python3 -c 'import json,sys; [print(w["leaf"], w["title"], "class="+w.get("class",""), "ws="+w["workspace"], "focused" if w["focused"] else "") for w in json.load(sys.stdin)["data"]]'
echo "--- Mod4-3 then Mod4-b (back and forth) ---"
DISPLAY=$D xdotool key --clearmodifiers super+3; sleep 0.6; cur
DISPLAY=$D xdotool key --clearmodifiers super+b; sleep 0.6; cur
echo "--- go to ws 1; focus arrows ---"
DISPLAY=$D xdotool key --clearmodifiers super+1; sleep 0.6; cur
DISPLAY=$D xdotool key --clearmodifiers super+Left; sleep 0.4; foc
DISPLAY=$D xdotool key --clearmodifiers super+Right; sleep 0.4; foc
echo "--- move right via IPC then windows ---"
ipc '{"q":"move","dir":"right"}'
echo "--- theme toggle binding Mod4-t (dark → paper) ---"
DISPLAY=$D xdotool key --clearmodifiers super+t; sleep 0.8; ipc '{"q":"theme"}'
DISPLAY=$D xdotool key --clearmodifiers super+t; sleep 0.8; ipc '{"q":"theme"}'
DISPLAY=$D xdotool key --clearmodifiers super+t; sleep 0.8; ipc '{"q":"theme"}'
