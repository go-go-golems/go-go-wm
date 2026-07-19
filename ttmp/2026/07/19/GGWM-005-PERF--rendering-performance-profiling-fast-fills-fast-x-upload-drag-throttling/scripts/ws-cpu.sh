#!/bin/bash
# Measure CPU seconds (not wall time) the WM spends creating workspaces.
set -u
export DISPLAY=:85
BIN=/tmp/claude-1000/go-go-wm
RUN=/tmp/claude-1000/ggwm-wscpu
mkdir -p "$RUN"
export GO_GO_WM_SOCKET="$RUN/wm.sock"
cleanup() {
  pkill -f "Xvfb :85" 2>/dev/null
  pkill -f "go-go-wm wm --display :85" 2>/dev/null
}
case "${1:-run}" in stop) cleanup; exit 0;; esac
cleanup; sleep 0.5
Xvfb :85 -screen 0 1280x800x24 >/dev/null 2>&1 &
sleep 1
cd /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm

# Stage A: boot WITHOUT rc (no workspace creation) to get baseline CPU.
GO_GO_WM_PPROF=localhost:6061 $BIN wm --display :85 --embedded-broker --socket "$RUN/broker.sock" \
  --ipc-socket "$GO_GO_WM_SOCKET" --theme dark >/dev/null 2>&1 &
WMPID=$!
for _ in $(seq 40); do [ -S "$GO_GO_WM_SOCKET" ] && break; sleep 0.25; done
sleep 1
cpu() { awk '{print ($14+$15)/100}' /proc/$WMPID/stat; }
BASE=$(cpu)
T0=$(date +%s.%N)
curl -s -o "$RUN/boot.prof" "http://localhost:6061/debug/pprof/profile?seconds=6" &
CURLPID=$!
sleep 0.3

# Stage B: create 8 workspaces + rename via IPC (same ops i3.js issues).
python3 - <<'EOF'
import json, socket, os
s = socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
f = s.makefile()
def q(req):
    s.sendall((json.dumps(req)+"\n").encode()); return json.loads(f.readline())
for i in range(2, 10):
    r = q({"q":"op","op":{"op":"add-workspace"}})
    ws = r["data"]["new_workspace"]
    q({"q":"op","op":{"op":"rename-workspace","workspace":ws,"name":str(i)}})
q({"q":"op","op":{"op":"switch-workspace","workspace":"ws1"}})
EOF
T1=$(date +%s.%N)
AFTER=$(cpu)
echo "8 workspaces + renames + switch-home:"
python3 -c "print(f'  wall: {$T1-$T0:.2f}s   WM CPU: {$AFTER-$BASE:.2f}s   per add+rename: {($AFTER-$BASE)/8*1000:.0f}ms CPU')"
echo "system load: $(cut -d' ' -f1-3 /proc/loadavg) on $(nproc) cores"
wait $CURLPID
go tool pprof -top -nodecount=12 "$BIN" "$RUN/boot.prof" 2>/dev/null | tail -14
echo "--- cum ---"
go tool pprof -top -cum -nodecount=12 "$BIN" "$RUN/boot.prof" 2>/dev/null | tail -14
cleanup
