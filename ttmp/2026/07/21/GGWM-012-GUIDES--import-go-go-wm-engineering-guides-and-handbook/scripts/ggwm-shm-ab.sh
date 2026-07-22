#!/bin/bash
# ggwm-shm-ab.sh — A/B the MIT-SHM resize hypothesis (GGWM-012, Lab 3).
#
# Hypothesis under test: paintFrame destroys and recreates the shm surface on
# every dimension change (manage.go:420-432), and xshm.New issues two CHECKED
# X requests (xshm.go:92, :106) — i.e. two synchronous round trips per resized
# pane per motion tick. If true, disabling shm should NOT make drags much worse
# and may make them better, because the fallback path avoids the churn.
#
# Runs INSIDE an X session. Start it with:
#   startx ~/.xinitrc.ggwm-shmtest -- :2 vt2 -config ~/.xorg.go-go-wm.conf
#
# Produces two JSON logs under $OUT for offline comparison.
set -uo pipefail

# Capture everything this script says. The first run of this harness ended
# with the X session closing and no explanation, because xterm -e swallows
# stdout/stderr and xinit tears the server down as soon as its client exits.
OUT="${OUT:-$HOME/ggwm-shm-ab}"
mkdir -p "$OUT"
exec > >(tee -a "$OUT/harness.log") 2>&1
echo "=== harness start $(date -Is) DISPLAY=${DISPLAY:-unset} ==="
trap 'echo "=== harness EXIT rc=$? at line $LINENO ==="; echo "(window stays open; press Enter)"; read -r _ || true' EXIT
set -x

GO_GO_WM="${GO_GO_WM:-$HOME/.local/bin/go-go-wm}"
SOCK="$XDG_RUNTIME_DIR/go-go-wm.sock"
PBUI_SOCK="$XDG_RUNTIME_DIR/pbui.sock"
REPS="${REPS:-3}"        # drag sweeps per condition
# Screen is 1280x800 (see ~/.xorg.go-go-wm.conf "Virtual 1280 800"), so a single
# vertical split at ratio 0.5 puts the divider at x~640. Sweep across it.
DIVX=640; DIVY=400
SWEEP_LO=380; SWEEP_HI=900; STEP=6; DELAY=0.004   # ~250 samples/sec

ipc() {  # ipc '<json>'  -> prints the response line
  python3 - "$SOCK" "$1" <<'PY'
import socket,sys
s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
s.settimeout(5); s.connect(sys.argv[1])
s.sendall((sys.argv[2]+"\n").encode())
print(s.makefile().readline().strip())
PY
}

first_leaf() {  # print the first leaf NodeID of the current workspace
  ipc '{"q":"tree"}' | python3 -c '
import sys,json
r=json.loads(sys.stdin.read())
d=r.get("data") or {}
def walk(n):
    if not isinstance(n,dict): return None
    if n.get("kind")=="leaf": return n.get("id")
    for k in ("a","b"):
        v=walk(n.get(k))
        if v: return v
    return None
wss=d.get("workspaces") or []
cur=d.get("current")
for w in wss:
    if not cur or w.get("id")==cur:
        print(walk(w.get("root")) or ""); break
'
}

run_condition() {
  local label="$1" noshm="$2"
  local log="$OUT/$label.jsonl"
  echo "=== condition: $label (GO_GO_WM_NO_SHM=$noshm) ==="
  rm -f "$SOCK" "$PBUI_SOCK" "$log"

  GO_GO_WM_NO_SHM="$noshm" "$GO_GO_WM" wm \
      --display "$DISPLAY" --embedded-broker --spawn xterm \
      --log-level debug --log-format json --log-file "$log" &
  local wmpid=$!

  for i in $(seq 1 100); do [ -S "$SOCK" ] && break; sleep 0.1; done
  [ -S "$SOCK" ] || { echo "!! socket never appeared"; kill $wmpid 2>/dev/null; return 1; }
  sleep 2   # let the first xterm map

  local leaf; leaf=$(first_leaf)
  if [ -n "$leaf" ]; then
    echo "-- splitting leaf $leaf"
    ipc "{\"q\":\"op\",\"op\":{\"op\":\"split-leaf\",\"node\":\"$leaf\",\"dir\":\"row\"}}" >/dev/null
    ( DISPLAY="$DISPLAY" xterm & ) 2>/dev/null
    sleep 2
  else
    echo "!! could not find a leaf; dragging anyway"
  fi

  echo "-- marker: drag starts"
  echo "{\"__marker\":\"drag_start\",\"cond\":\"$label\"}" >> "$log"
  for r in $(seq 1 "$REPS"); do
    xdotool mousemove $DIVX $DIVY
    sleep 0.2
    xdotool mousedown 1
    for x in $(seq $SWEEP_LO $STEP $SWEEP_HI); do xdotool mousemove $x $DIVY; sleep $DELAY; done
    for x in $(seq $SWEEP_HI -$STEP $SWEEP_LO); do xdotool mousemove $x $DIVY; sleep $DELAY; done
    xdotool mousemove $DIVX $DIVY
    xdotool mouseup 1
    sleep 0.5
  done
  echo "{\"__marker\":\"drag_end\",\"cond\":\"$label\"}" >> "$log"
  echo "-- marker: drag ends"

  # Bounded aggregate counters (GGWM-012 perf IPC query) before shutdown.
  ipc '{"q":"perf"}' > "$OUT/$label.perf.json" 2>/dev/null || true
  echo "-- perf: $(cat "$OUT/$label.perf.json" 2>/dev/null | head -c 400)"
  sleep 1
  kill $wmpid 2>/dev/null; wait $wmpid 2>/dev/null
  sleep 1
  echo "-- wrote $log ($(wc -l < "$log") lines)"
}

run_condition shm-on  ""
run_condition shm-off 1

echo
echo "DONE. Logs in $OUT:"
ls -la "$OUT"
echo
echo "Press Enter to end the X session."
read -r _
