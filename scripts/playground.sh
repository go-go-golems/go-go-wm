#!/usr/bin/env bash
# playground: a one-command demo session for assessing go-go-wm.
#
# Starts Xephyr (a nested X server window on your real desktop), the WM
# with the embedded broker, the rich REPL, demo apps, a floating dialog,
# and a script daemon that serves a launcher command + verb — then
# prints a cheat sheet of keys, gestures, and commands to exercise every
# subsystem. Ctrl-C tears everything down.
#
# Usage:
#   scripts/playground.sh              # default binds (Mod4-d launcher, …)
#   scripts/playground.sh --i3         # i3.js config (your ported i3 keys)
#   scripts/playground.sh --theme dark # any of: paper | light | dark
#   scripts/playground.sh -d 7         # use display :7 (default :5)
set -euo pipefail

DPY_NUM=5
RC=""
THEME="paper"
while [ $# -gt 0 ]; do
  case "$1" in
    --i3) RC="i3"; shift ;;
    --theme) THEME="$2"; shift 2 ;;
    -d|--display) DPY_NUM="$2"; shift 2 ;;
    *) echo "unknown flag $1"; exit 2 ;;
  esac
done
DPY=":$DPY_NUM"
cd "$(dirname "$0")/.."

BIN="${GO_GO_WM_BIN:-/tmp/ggwm-playground/go-go-wm}"
PBUI_SOCK="/tmp/ggwm-playground-$DPY_NUM-pbui.sock"
WM_SOCK="/tmp/ggwm-playground-$DPY_NUM-wm.sock"
LOG="/tmp/ggwm-playground-$DPY_NUM-logs"
mkdir -p "$LOG" "$(dirname "$BIN")"

PIDS=()
cleanup() {
  echo
  echo "shutting down playground…"
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  sleep 0.5
  for p in "${PIDS[@]:-}"; do kill -9 "$p" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM

echo "building go-go-wm…"
go build -o "$BIN" ./cmd/go-go-wm

rm -f "$PBUI_SOCK" "$WM_SOCK"

if [ -n "${PLAYGROUND_HEADLESS:-}" ]; then
  echo "starting Xvfb on $DPY (headless test mode)…"
  Xvfb "$DPY" -screen 0 1600x900x24 >"$LOG/xephyr.log" 2>&1 &
else
  command -v Xephyr >/dev/null || { echo "Xephyr not installed (apt install xserver-xephyr)"; exit 1; }
  echo "starting Xephyr on $DPY (1600x900)…"
  # NOTE: no -no-host-grab — that flag disables Xephyr's Ctrl+Shift
  # grab toggle entirely. Default Xephyr: press Ctrl+Shift inside the
  # window to grab keyboard+mouse (Mod4 then reaches the nested WM),
  # Ctrl+Shift again to release.
  Xephyr "$DPY" -screen 1600x900 -title "go-go-wm playground" \
    >"$LOG/xephyr.log" 2>&1 &
fi
PIDS+=($!)
sleep 1.5

WM_ARGS=(wm --display "$DPY" --embedded-broker
  --socket "$PBUI_SOCK" --ipc-socket "$WM_SOCK" --theme "$THEME")
if [ "$RC" = "i3" ]; then
  WM_ARGS+=(--no-default-binds --rc examples/scripts/i3.js)
fi
echo "starting the WM (theme: $THEME${RC:+, config: i3.js})…"
"$BIN" "${WM_ARGS[@]}" >"$LOG/wm.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 40); do [ -S "$WM_SOCK" ] && break; sleep 0.25; done
[ -S "$WM_SOCK" ] || { echo "WM never came up:"; tail -20 "$LOG/wm.log"; exit 1; }
sleep 1

echo "populating the desktop…"
# A terminal and the rich REPL as tiles.
DISPLAY="$DPY" xterm -fa Monospace -fs 11 >"$LOG/xterm.log" 2>&1 &
PIDS+=($!)
sleep 0.7
DISPLAY="$DPY" "$BIN" repl --ui --socket "$PBUI_SOCK" --wm-socket "$WM_SOCK" \
  >"$LOG/repl.log" 2>&1 &
PIDS+=($!)
sleep 0.7
# Demo apps (PBUI clients: chips, verbs, accepts).
for app in colors numbers; do
  "$BIN" demo --app "$app" --display "$DPY" --socket "$PBUI_SOCK" \
    >"$LOG/demo-$app.log" 2>&1 &
  PIDS+=($!)
  sleep 0.5
done
# A floating dialog (WM_TRANSIENT_FOR-less, but typed as a dialog).
"$BIN" testwin --display "$DPY" --type dialog --title "sample dialog" \
  --size 360x180 >"$LOG/testwin.log" 2>&1 &
PIDS+=($!)
# A script daemon: launcher command (A2) + a color verb + printed chips.
cat > "$LOG/playground-daemon.js" <<'EOF'
const wm = require("wm");
const pbui = require("pbui");
wm.command({ id: "demo-layout", label: "demo: build a 3-pane workspace",
             doc: "served by playground-daemon.js over the broker",
             run() {
               const ws = wm.workspace("demo");
               ws.switch();
             } });
pbui.verb({ id: "demo.shout", label: "Shout (playground verb)",
            ptypes: ["color"] },
          function (obj) {
            pbui.print("the playground daemon received ",
                       pbui.object("color", obj.value));
          });
pbui.print("playground daemon up — try: ",
           pbui.object("color", "#c2543a"), " ",
           pbui.object("number", 42));
EOF
GO_GO_WM_SOCKET="$WM_SOCK" "$BIN" run "$LOG/playground-daemon.js" \
  --socket "$PBUI_SOCK" --wm-socket "$WM_SOCK" >"$LOG/daemon.log" 2>&1 &
PIDS+=($!)
sleep 1

# Convenience wrappers for the cheat sheet.
SOCKS="--socket $PBUI_SOCK"
W="--wm-socket $WM_SOCK"

cat <<SHEET

════════════════════════════════════════════════════════════════════════
  go-go-wm playground is up on $DPY   (logs: $LOG)
  Press Ctrl+Shift INSIDE the Xephyr window to grab keyboard+mouse —
  otherwise your host WM eats Mod4. Ctrl+Shift again releases the grab.
════════════════════════════════════════════════════════════════════════

KEYBOARD $( [ "$RC" = "i3" ] && echo "(i3.js config)" || echo "(default binds)" )
$(if [ "$RC" = "i3" ]; then cat <<'I3KEYS'
  Mod4+Return        kitty terminal        Mod4+d          launcher popup
  Mod4+h / Mod4+v    split right / down    Mod4+Shift+q    close window
  Mod4+arrows        directional focus     Mod4+Shift+arrows  move window
  Mod4+1..9          switch workspace      Mod4+Shift+1..9 move + follow
  Mod4+b             back-and-forth        Mod4+t          cycle theme
  Mod4+Shift+space   float toggle          Mod4+f          fullscreen toggle
  Mod4+p / Print     flameshot
I3KEYS
else cat <<'DEFKEYS'
  Mod4+Return        xterm                 Mod4+d          launcher popup
  Mod4+Shift+d       split right           Mod4+s          split below
  Mod4+w             close tile            Mod4+space      focus next
  Mod4+f             fullscreen toggle     Mod4+1..9       switch workspace
  Mod4+n             new workspace         Mod4+Shift+q    quit the WM
  Escape             cancel accept / close menu / clear launcher query
DEFKEYS
fi)

MOUSE
  click title strip        focus            drag ⠿ grip      swap / dock tiles
  drag divider             resize (sticky)  drag float strip move the float
  left-click a chip        primary action / answer a pending accept
  right-click a chip       verb menu (incl. "Shout" from the daemon, on colors)

THE LAUNCHER (one registry, two surfaces)
  · press Mod4+d and type — fuzzy filter over apps/builtins/commands
  · try typing:  repl   trace   demo   (Enter launches, Esc closes)
  · "demo: build a 3-pane workspace" is served LIVE by the script daemon
  · any empty tile is also a launcher: just start typing into it

THE RICH REPL (the "repl" tile)
  type into it and press Enter:
    1+1                          Out[1] is a live number
    "#aa5533"                    a clickable swatch (answers accepts!)
    [3,1,4,1,5,9,2,6]            a series — click the view buttons (bars/table/json)
    [{a:1,b:2},{a:3,b:4}]        a dataset with table + schema views
    Out(2)                       history is live;  $_ is the last value
    wm.tree()                    the desktop as data
    wm.theme("dark")             restyle everything live
  Up/Down = input history · PgUp/PgDn = scroll · right-click Out chips

FLOATS & FULLSCREEN
  · "sample dialog" floats (dialog-typed) — drag it by its strip
  · Mod4+f fullscreens the focused window; Mod4+f again restores
  · float toggle: $( [ "$RC" = "i3" ] && echo "Mod4+Shift+space" || echo "run: $BIN query … or bind it" )

FROM ANOTHER TERMINAL (the desktop as an API)
  export GO_GO_WM_SOCKET=$WM_SOCK
  $BIN accept --ptype color $SOCKS       # then click ANY swatch in Xephyr
  $BIN accept --ptype command $SOCKS     # launcher surfaces become pickers
  $BIN query tree $W --output json       # what the WM believes
  $BIN query windows $W                  # incl. floating column
  $BIN query events --count 10 $SOCKS    # follow the bus (ops, accepts, cells)
  $BIN query verbs --ptype color $SOCKS  # who serves the menu
  echo '{"q":"launch","target":"script:demo-layout"}' | python3 -c "import socket,sys;s=socket.socket(socket.AF_UNIX);s.connect('$WM_SOCK');s.sendall(sys.stdin.read().encode());print(s.makefile().readline())"
  $BIN repl $SOCKS $W                    # terminal REPL against this desktop

THINGS THAT SHOW THE POINT
  1. Run the accept command above, then click a color in the colors
     demo, the REPL's Out chip, or the daemon's printed chip — any
     surface answers, because a chip IS the object.
  2. Right-click a number in the numbers demo → "Multiply by…" → click
     another number anywhere. Verbs + accepts compose across processes.
  3. Open the trace tile (launcher → "trace"): every op and event.

Ctrl-C here stops everything.
════════════════════════════════════════════════════════════════════════
SHEET

wait
