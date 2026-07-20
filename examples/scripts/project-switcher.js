// project-switcher.js — one keypress, a whole working context. [P4: layouts]
//
//   go-go-wm run --once examples/scripts/project-switcher.js
//
// Layout recipes are data: normalize→compile turns the nested spec into
// a plan of split-leaf/set-ratio/set-leaf-app Ops, inspectable via
// wm.layouts() before anything executes. Running the recipe twice is
// idempotent — the workspace already exists, so it just switches.

const wm = require("wm");
const pbui = require("pbui");

wm.layout("dev", {
  split: "row", ratio: 0.62,
  a: { app: "editor" },
  b: {
    split: "col", ratio: 0.5,
    a: { app: "terminal" },
    b: { app: "notes" },
  },
});

const ws = wm.workspace("go-go-wm");
ws.switch();
ws.apply("dev");

pbui.print("project go-go-wm ready: editor | terminal / notes");
