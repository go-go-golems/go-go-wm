// rc.js — the in-process startup script. [works: P3]
//
//   go-go-wm wm --display :1 --embedded-broker --rc examples/scripts/rc.js
//
// This file runs *inside* the WM process, in the WM's own goja runtime,
// after the WM takes the display. Keybindings are only possible here
// (a standalone script has no X connection — wm.bind explains that if
// you try). Everything else is the same API as standalone scripts:
// "develop in the REPL, deploy in rc.js" is a copy-paste.

const wm = require("wm");
const pbui = require("pbui");

// Keybindings compile to the same Ops as the built-in ones.
wm.bind("Mod4-e", () => wm.split(wm.focused() || wm.leaves()[0].id, "row"));
wm.bind("Mod4-Shift-e", () => wm.split(wm.focused() || wm.leaves()[0].id, "col"));

// A custom verb, available on every tile's right-click menu.
pbui.verb(
  { id: "tile.note", label: "Note this tile", ptypes: ["tile"] },
  (tile) => pbui.print("noted tile ", pbui.object("tile", tile.value)),
);

// P4 preview: declarative placement rules, normalized and inspectable.
// wm.rule({ title: /zoom/i, workspace: "calls" });

pbui.print("rc.js loaded — Mod4-e splits right, Mod4-Shift-e splits below");
