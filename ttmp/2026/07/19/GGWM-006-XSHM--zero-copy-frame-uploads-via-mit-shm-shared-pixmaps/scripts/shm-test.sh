#!/bin/bash
set -u
export DISPLAY=:86
BIN=/tmp/claude-1000/go-go-wm
RUN=/tmp/claude-1000/ggwm-shm
mkdir -p "$RUN"
export GO_GO_WM_SOCKET="$RUN/wm.sock"
cleanup() {
  pkill -f "Xvfb :86" 2>/dev/null
  pkill -f "go-go-wm wm --display :86" 2>/dev/null
  pkill -x xterm 2>/dev/null
}
case "${1:-run}" in stop) cleanup; exit 0;; esac
cleanup; sleep 0.5
Xvfb :86 -screen 0 1280x800x24 >/dev/null 2>&1 &
sleep 1
cd /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm

boot() { # boot [extra-env]
  env $1 $BIN wm --display :86 --embedded-broker --socket "$RUN/broker.sock" \
    --ipc-socket "$GO_GO_WM_SOCKET" --theme dark >"$RUN/wm.log" 2>&1 &
  WMPID=$!
  for _ in $(seq 40); do [ -S "$GO_GO_WM_SOCKET" ] && break; sleep 0.25; done
  sleep 1
}
op() { python3 -c '
import json,socket,os,sys
s=socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
s.sendall((sys.argv[1]+"\n").encode()); print(s.makefile().readline().strip()[:80])' "$1"; }
scene() { # builtin tiles for deterministic content
  op '{"q":"op","op":{"op":"set-leaf-app","node":"n1","app":"builtin:trace"}}' >/dev/null
  op '{"q":"op","op":{"op":"split-leaf","node":"n1","dir":"row","app":"builtin:listener"}}' >/dev/null
  sleep 1
}

echo "== SHM path =="
boot "GO_GO_WM_XXX=1"
grep "frame upload path" "$RUN/wm.log"
scene
import -window root "$RUN/shm.png"
ipcs -m | awk 'NR>3 && $5>1000000 {n++} END {print "large shm segments while running:", n+0}'
kill -9 $WMPID; sleep 1
ipcs -m | awk 'NR>3 && $5>1000000 {n++} END {print "large shm segments after kill -9:", n+0}'
pkill -f "go-go-wm wm --display :86" 2>/dev/null; sleep 0.5

echo "== fallback path (GO_GO_WM_NO_SHM=1) =="
boot "GO_GO_WM_NO_SHM=1"
grep "frame upload path" "$RUN/wm.log"
scene
import -window root "$RUN/noshm.png"
kill $WMPID 2>/dev/null; sleep 0.5

python3 - <<'EOF'
from PIL import Image, ImageChops
a = Image.open("/tmp/claude-1000/ggwm-shm/shm.png").convert("RGB")
b = Image.open("/tmp/claude-1000/ggwm-shm/noshm.png").convert("RGB")
diff = ImageChops.difference(a, b).getbbox()
print("pixel diff bbox (None = identical):", diff)
EOF
cleanup
echo SHMTEST-DONE
