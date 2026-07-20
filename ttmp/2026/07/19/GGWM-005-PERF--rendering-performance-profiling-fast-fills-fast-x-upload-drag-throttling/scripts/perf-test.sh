#!/bin/bash
# GGWM-004 perf: profile boot workspace-creation + divider drag.
set -u
export DISPLAY=:84
BIN=/tmp/claude-1000/go-go-wm
RUN=/tmp/claude-1000/ggwm-perf
mkdir -p "$RUN"
export GO_GO_WM_SOCKET="$RUN/wm.sock"
export GO_GO_WM_PPROF=localhost:6060
cleanup() {
  pkill -f "Xvfb :84" 2>/dev/null
  pkill -f "go-go-wm wm --display :84" 2>/dev/null
  pkill -x xterm 2>/dev/null
}
case "${1:-run}" in stop) cleanup; exit 0;; esac
cleanup; sleep 0.5
Xvfb :84 -screen 0 1280x800x24 >/dev/null 2>&1 &
sleep 1
cd /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm

# Boot WITH debug logging: the afterOp/paintFrame timings cover startup.
t0=$(date +%s.%N)
$BIN --log-level debug wm --display :84 --embedded-broker --socket "$RUN/broker.sock" \
  --ipc-socket "$GO_GO_WM_SOCKET" --theme dark --no-default-binds \
  --rc examples/scripts/i3.js >"$RUN/wm.log" 2>&1 &
# wait for all 9 workspaces
python3 - <<'EOF'
import json, socket, os, time
t0 = time.time()
while time.time() - t0 < 30:
    try:
        s = socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
        s.sendall(b'{"q":"tree"}\n')
        d = json.loads(s.makefile().readline())["data"]
        if len(d["workspaces"]) == 9 and d["current"] == d["workspaces"][0]["id"]:
            print(f"boot: 9 workspaces ready after {time.time()-t0:.2f}s")
            break
    except Exception:
        pass
    time.sleep(0.1)
EOF

echo "== startup timing breakdown (afterOp) =="
grep afterOp "$RUN/wm.log" | python3 -c '
import sys, re, collections
tot = collections.Counter(); cnt = collections.Counter()
for l in sys.stdin:
    m = re.search(r"ms=(\S+?) op=(\S+)", l)
    if not m: continue
    v, op = m.groups()
    ms = float(v[:-2]) if v.endswith("ms") else float(v[:-1])*1000 if v.endswith("s") else 0
    tot[op] += ms; cnt[op] += 1
for op, ms in tot.most_common():
    print(f"  {op:20s} n={cnt[op]:3d} total={ms:8.1f}ms avg={ms/cnt[op]:7.1f}ms")'
echo "== slowest paintFrames =="
grep paintFrame "$RUN/wm.log" | sed -E 's/.*ms=([0-9.]+[a-z]+).*leaf=(\S+) w=(\S+) h=(\S+).*/\1 \2 \3x\4/' | sort -rn | head -5

# Two xterms so ws1 has a divider to drag.
setsid xterm -display :84 -T left >/dev/null 2>&1 </dev/null &
sleep 1
setsid xterm -display :84 -T right >/dev/null 2>&1 </dev/null &
sleep 1.5

# Start a 12s CPU profile, then drag the divider back and forth.
curl -s -o "$RUN/cpu.prof" "http://localhost:6060/debug/pprof/profile?seconds=12" &
CURL_PID=$!
sleep 0.5
# divider is at x ~ 640 between the tiles; press, sweep, release, x3
for pass in 1 2 3; do
  xdotool mousemove 640 400 mousedown 1
  for x in $(seq 500 8 800); do xdotool mousemove $x 400; done
  for x in $(seq 800 -8 640); do xdotool mousemove $x 400; done
  xdotool mouseup 1
done
wait $CURL_PID

echo "== click-to-focus: click INSIDE the left client area =="
python3 -c '
import json,socket,os
s=socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
s.sendall(b"{\"q\":\"windows\"}\n")
[print("focused before:", w["title"]) for w in json.loads(s.makefile().readline())["data"] if w["focused"]]'
xdotool mousemove 200 400 click 1; sleep 0.6
python3 -c '
import json,socket,os
s=socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
s.sendall(b"{\"q\":\"windows\"}\n")
[print("focused after left-click:", w["title"]) for w in json.loads(s.makefile().readline())["data"] if w["focused"]]'
xdotool mousemove 1000 400 click 1; sleep 0.6
python3 -c '
import json,socket,os
s=socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
s.sendall(b"{\"q\":\"windows\"}\n")
[print("focused after right-side click:", w["title"]) for w in json.loads(s.makefile().readline())["data"] if w["focused"]]'

echo "== profile captured: $RUN/cpu.prof =="
go tool pprof -top -nodecount=15 "$BIN" "$RUN/cpu.prof" 2>/dev/null | head -22
echo "== cumulative =="
go tool pprof -top -cum -nodecount=15 "$BIN" "$RUN/cpu.prof" 2>/dev/null | head -22
echo DONE
