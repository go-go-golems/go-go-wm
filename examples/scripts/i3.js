// i3.js — the user's ~/.config/i3/config ported to go-go-wm (GGWM-004).
//
// Run it as the whole config:
//
//   go-go-wm wm --display :1 --embedded-broker \
//     --theme dark --no-default-binds --rc examples/scripts/i3.js
//
// --no-default-binds matters: this file owns the keyboard, i3-style, and
// double-grabbed combos would fire twice (Mod4-Shift-q would kill the
// window AND the WM).
//
// Line-by-line mapping from the i3 config:
//
//   i3                                     here
//   ------------------------------------- -----------------------------------
//   set $mod Mod4                          MOD constant below
//   bindsym $mod+Return exec kitty         wm.bind + wm.exec
//   bindsym $mod+Shift+q kill              wm.close(wm.focused())
//   bindsym $mod+<arrow> focus <dir>       wm.focus("left"|"right"|"up"|"down")
//   bindsym $mod+Shift+<arrow> move <dir>  wm.move(dir)
//   bindsym $mod+h / $mod+v split h/v      wm.split(wm.focused(), "row"/"col")
//   bindsym $mod+1..9 workspace N          wm.workspace("N").switch()
//   bindsym $mod+Shift+1..9 move+follow    ws.adopt(focused); ws.switch()
//   bindsym $mod+Ctrl+1..9 move           ws.adopt(focused)
//   workspace_auto_back_and_forth          tracked via wm.on("switch-workspace")
//   bindsym $mod+b back_and_forth          switch to the tracked previous
//   bindsym $mod+p / Print exec flameshot  wm.bind + wm.exec
//   bindsym $mod+d exec launcher.sh        wm.launcher() (the WM popup, GGWM-008)
//   assign [class="Slack"] 8               wm.rule({class: /Slack/, workspace: "8"})
//   assign [class="Emacs"] 1               wm.rule({class: /Emacs/, workspace: "1"})
//   assign [class="obsidian"] 2            wm.rule({class: /obsidian/, workspace: "2"})
//   assign [class="jetbrains-idea"] 3      wm.rule({class: /jetbrains-idea/, workspace: "3"})
//   for_window [...] floating enable       wm.rule({class/title: …, float: true})  (GGWM-007)
//   bindsym $mod+Shift+space floating…     wm.float()
//   client.background #1f1f1f              --theme dark (same anchor color)
//   exec --no-startup-id …                 AUTOSTART below (opt-in via env)
//
//   NOT PORTED (no WM mechanism yet): scratchpad/sticky,
//   stacking/tabbed layouts, fullscreen toggle, i3 modes (resize/gaps/
//   system — global grabs of bare letters would swallow app keys),
//   multi-output workspace pinning, i3bar/polybar, border styles,
//   urgency focus. Resize lives on the mouse (sticky dividers);
//   floats also honor client resize requests (ConfigureRequest).

var wm = require("wm");

var MOD = "Mod4";
var TERMINAL = "kitty";

// --- numbered workspaces --------------------------------------------------
// i3 creates workspaces on demand; here we pre-create 1..9 so names,
// bar chips, and rules all agree from the start. The bootstrap
// workspace becomes "1".
// Two batches instead of 17 individual ops: adds first (their results
// carry the new ids), then renames + come-home — the WM reconciles and
// paints once per batch (GGWM-006), so pre-creation is near-free.
var first = wm.tree().workspaces[0];
var adds = [];
for (var i = 2; i <= 9; i++) adds.push({ op: "add-workspace" });
var created = wm.apply(adds);
var finish = [{ op: "rename-workspace", workspace: first.id, name: "1" }];
for (var j = 0; j < created.length; j++) {
  finish.push({ op: "rename-workspace", workspace: created[j].new_workspace, name: String(j + 2) });
}
finish.push({ op: "switch-workspace", workspace: first.id });
wm.apply(finish);

// --- workspace back-and-forth ---------------------------------------------
var currentWs = wm.tree().current;
var previousWs = currentWs;
wm.on("switch-workspace", function (ev) {
  var ws = ev.data && ev.data.workspace;
  if (ws && ws !== currentWs) {
    previousWs = currentWs;
    currentWs = ws;
  }
});

function bind(combo, fn) {
  wm.bind(MOD + "-" + combo, fn);
}

// --- launchers ------------------------------------------------------------
// i3: bindsym $mod+d exec launcher.sh — here the WM's own popup.
bind("d", function () { wm.launcher(); });
bind("Return", function () { wm.exec(TERMINAL); });
bind("p", function () { wm.exec("flameshot gui"); });
wm.bind("Print", function () { wm.exec("flameshot gui"); });
bind("Shift-d", function () { wm.exec(TERMINAL + ' --title "edit-clipboard" -e ~/.local/bin/ec'); });

// --- window management ----------------------------------------------------
bind("Shift-q", function () {
  var f = wm.focused();
  if (f) wm.close(f);
});
bind("Left", function () { wm.focus("left"); });
bind("Down", function () { wm.focus("down"); });
bind("Up", function () { wm.focus("up"); });
bind("Right", function () { wm.focus("right"); });
bind("Shift-Left", function () { wm.move("left"); });
bind("Shift-Down", function () { wm.move("down"); });
bind("Shift-Up", function () { wm.move("up"); });
bind("Shift-Right", function () { wm.move("right"); });
bind("h", function () { var f = wm.focused(); if (f) wm.split(f, "row"); });
bind("v", function () { var f = wm.focused(); if (f) wm.split(f, "col"); });
bind("space", function () { wm.focus("next"); });
// toggle tiling / floating (i3 $mod+Shift+space)
bind("Shift-space", function () { wm.float(); });

// --- workspaces -----------------------------------------------------------
for (var n = 1; n <= 9; n++) {
  (function (name) {
    // switch
    bind(String(name), function () { wm.workspace(name).switch(); });
    // move focused container + follow (i3 $mod+Shift+N)
    bind("Shift-" + name, function () {
      var f = wm.focused();
      var ws = wm.workspace(name);
      if (f) ws.adopt(f);
      ws.switch();
    });
    // move without following (i3 $mod+Ctrl+N)
    bind("Control-" + name, function () {
      var f = wm.focused();
      if (f) wm.workspace(name).adopt(f);
    });
  })(String(n));
}
bind("b", function () { wm.workspace(previousWs).switch(); });
bind("Shift-b", function () {
  var f = wm.focused();
  var ws = wm.workspace(previousWs);
  if (f) ws.adopt(f);
  ws.switch();
});

// --- theme toggle (bonus: i3 has no runtime theming) ----------------------
bind("t", function () {
  var order = { paper: "light", light: "dark", dark: "paper" };
  wm.theme(order[wm.theme()] || "paper");
});

// --- assigns --------------------------------------------------------------
wm.rule({ class: /Slack/, workspace: "8" });
wm.rule({ class: /Emacs/, workspace: "1" });
wm.rule({ class: /obsidian/, workspace: "2" });
wm.rule({ class: /jetbrains-idea/, workspace: "3" });

// --- floating rules (GGWM-007) --------------------------------------------
// The i3 config's `for_window [...] floating enable` list. Border
// styles, sticky, and `resize set` have no equivalent yet; the float
// itself is the part that matters daily. Dialogs and fixed-size
// windows float automatically (WM_TRANSIENT_FOR, _NET_WM_WINDOW_TYPE,
// min==max hints) — these rules cover apps that set none of those.
[
  /Calamares/, /Clipgrab/, /Galculator/, /GParted/,
  /Lightdm-gtk-greeter-settings/, /Lxappearance/, /Manjaro-hello/,
  /Manjaro Settings Manager/, /Nitrogen/, /octopi/, /Pamac-manager/,
  /Pavucontrol/, /qt5ct/, /Qtconfig-qt4/, /Simple-scan/,
  /System-config-printer.py/, /Skype/, /Thus/, /Timeset-gui/,
  /virtualbox/, /Xfburn/,
].forEach(function (cls) { wm.rule({ class: cls, float: true }); });
[
  /alsamixer/, /edit-clipboard/, /File Transfer/, /i3_help/,
  /MuseScore: Play Panel/, /About Pale Moon/, /^md-view:/,
].forEach(function (title) { wm.rule({ title: title, float: true }); });

// --- autostart ------------------------------------------------------------
// The i3 exec lines target the real machine (nitrogen, compton, polybar,
// redshift, copyq, …). Flip AUTOSTART_ENABLED on the machine this is
// deployed to; nested/test sessions stay clean by default.
var AUTOSTART_ENABLED = false;
var AUTOSTART = [
  "nm-applet",
  "copyq",
  'redshift -l "42.35:-71.05"',
  "setxkbmap -option caps:escape",
];
if (AUTOSTART_ENABLED) {
  AUTOSTART.forEach(function (cmd) { wm.exec(cmd); });
}

console.log("i3.js loaded: theme=" + wm.theme() + " workspaces=" +
  wm.tree().workspaces.length + " rules=" + wm.rules().length);
