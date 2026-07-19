// rc.js — the in-process startup script. [P3: --rc flag]
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

// Keybindings compile to the same verbs/Ops as the built-in ones.
wm.bind("Mod4-e", () => wm.split(wm.focused(), "row"));
wm.bind("Mod4-Shift-e", () => wm.split(wm.focused(), "col"));

// A custom verb, available on every tile's right-click menu.
pbui.verb(
  { id: "tile.note", label: "Note this tile", ptypes: ["tile"] },
  (tile) => pbui.print("noted tile ", pbui.object("tile", tile.value)),
);

// Declarative placement rules (P4): normalized and inspectable via
// wm.rules(), executed by the module when window.managed fires.
wm.rule({ app: /zoom/i, workspace: "calls", ratio: 0.5 });

pbui.print("rc.js loaded");
