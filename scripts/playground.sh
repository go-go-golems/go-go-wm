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
else
  # Default: an rc.js that builds the showcase grid, spawns the two
  # terminals into it, and registers a couple of verbs + a launcher
  # command. Default keybindings stay on (no --no-default-binds).
  cat > "$LOG/playground-rc.js" <<'RCEOF'
const wm = require("wm");
const pbui = require("pbui");

// Layout:  [ xterm | listener ]  over  [ kitty | inspector | trace ]
var topLeft = wm.leaves()[0].id;
var botLeft = wm.split(topLeft, "col", { ratio: 0.52 });
wm.split(topLeft, "row", { app: "builtin:listener", ratio: 0.5 });
var insp = wm.split(botLeft, "row", { app: "builtin:inspector", ratio: 0.5 });
wm.split(insp, "row", { app: "builtin:trace", ratio: 0.5 });

// Two terminals fill the empty leaves (top-left, bottom-left). kitty is
// the one to run  git log --oneline | go-go-wm scrape  in and click.
wm.exec("xterm -fa Monospace -fs 11");
wm.exec("kitty");

// A <color> verb, so color chips get an action beyond Describe.
pbui.verb({ id: "color.luminance", label: "Print luminance", ptypes: ["color"] },
  function (o) {
    var h = String(o.value).replace("#", "");
    var r = parseInt(h.slice(0, 2), 16), g = parseInt(h.slice(2, 4), 16), b = parseInt(h.slice(4, 6), 16);
    var lum = Math.round(((0.2126 * r + 0.7152 * g + 0.0722 * b) / 255) * 100) / 100;
    pbui.print(pbui.object("color", o.value), " luminance ", pbui.object("number", lum));
  });

// A launcher command that prints clickable, scrapeable objects.
wm.command({ id: "scrape-demo", label: "demo: print scrapeable objects",
  doc: "ip / url / git-commit / color chips into the listener",
  run: function () {
    pbui.print("try right-clicking these: ",
      pbui.object("ip", "8.8.8.8"), " ",
      pbui.object("url", "https://ruinwesen.com"), " ",
      pbui.object("git-commit", "5518e3f79b1df642b03bc1fe7d18062122d8eb63"), " ",
      pbui.object("color", "#c2543a"));
  } });

pbui.print("playground ready — in the kitty tile:  git log --oneline | go-go-wm scrape");
pbui.print("then click a hash · or Mod4-d → 'demo: print scrapeable objects' → right-click the chips");
RCEOF
  WM_ARGS+=(--rc "$LOG/playground-rc.js")
fi
echo "starting the WM (theme: $THEME${RC:+, config: i3.js})…"
"$BIN" "${WM_ARGS[@]}" >"$LOG/wm.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 40); do [ -S "$WM_SOCK" ] && break; sleep 0.25; done
[ -S "$WM_SOCK" ] || { echo "WM never came up:"; tail -20 "$LOG/wm.log"; exit 1; }
sleep 1

# The WM sets these in its own process env; export them here too so the
# xterm/repl/demos this SCRIPT spawns inherit them (clicked pbui:// links
# and in-terminal go-go-wm tools then reach this desktop's broker).
export PBUI_SOCKET="$PBUI_SOCK"
export GO_GO_WM_SOCKET="$WM_SOCK"

echo "loading desktop-wide verb daemons (git-commit, ip, url)…"
# The tiles and terminals come from the rc.js above. Here we load the
# example verb daemons as separate processes — so a git SHA / ip / url
# scraped in ANY terminal gains real right-click actions served from
# these daemons (the cross-process verb story). Each inherits the
# exported sockets.
for script in git-verbs net-verbs; do
  "$BIN" run "examples/scripts/$script.js" \
    --socket "$PBUI_SOCK" --wm-socket "$WM_SOCK" >"$LOG/$script.log" 2>&1 &
  PIDS+=($!)
  sleep 0.4
done
sleep 0.6

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

TILES ON SCREEN (default layout)
  [ xterm | listener ]  over  [ kitty | inspector | trace ]
  the two terminals are real X clients; listener/inspector/trace are
  WM-drawn builtins. Run scrape in the kitty tile (its config already
  has the pbui:// handler).

MOUSE
  click title strip        focus            drag ⠿ grip      swap / dock tiles
  drag divider             resize (sticky)  drag float strip move the float
  left-click a chip        primary action / answer a pending accept
  right-click a chip       verb menu (git/ip/url/color verbs are loaded)

★ THE SCRAPE → CLICK LOOP (the headline demo)
  In the kitty tile:
    git log --oneline | go-go-wm scrape       # hashes become clickable
    ip a | go-go-wm scrape                     # ip addresses too
  Click a link → the verb menu pops AT THE CURSOR with real actions:
    git-commit → Copy short hash · Compare with…   (git-verbs.js)
    ip         → Show octets · Reverse · Public or private?  (net-verbs.js)
  No terminal? Mod4+d → "demo: print scrapeable objects" → right-click
  the chips it prints into the listener. Actions print back into the
  listener (a live presentation you can click again).

THE LAUNCHER (one registry, two surfaces)
  · Mod4+d and type — fuzzy filter over apps / builtins / commands
  · try:  repl   trace   htop   firefox   (Enter launches, Esc closes)
  · any empty tile is also a launcher: just start typing into it

THE RICH REPL (launcher → "repl")
  type into it and press Enter:
    "#aa5533"                    a clickable swatch (answers accepts!)
    [3,1,4,1,5,9,2,6]            a series — click bars/table/json views
    [{a:1,b:2},{a:3,b:4}]        a dataset with table + schema views
    wm.tree()                    the desktop as data
    wm.theme("dark")             restyle every WM surface live (Mod4+t too)

FROM A TERMINAL INSIDE THE SESSION (xterm/kitty here, or Mod4+Return)
  the broker/control sockets are already exported — tools just work.
  For the scrape→click menu, once: 'go-go-wm kitty install' then reload
  kitty (Ctrl+Shift+F5). From an OUTSIDE terminal, first:
  export PBUI_SOCKET=$PBUI_SOCK  GO_GO_WM_SOCKET=$WM_SOCK
  go-go-wm accept --ptype color          # then click ANY swatch in Xephyr
  go-go-wm query events --count 10       # follow the bus (ops, verbs, accepts)
  go-go-wm query verbs --ptype ip        # who serves the menu
  go-go-wm scrape < somefile             # wrap objects in clickable links
  go-go-wm repl                          # terminal REPL against this desktop

Ctrl-C here stops everything.
════════════════════════════════════════════════════════════════════════
SHEET

wait
