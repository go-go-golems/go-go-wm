// hello.js — the smallest possible PBUI script. [works today: P1]
//
//   go-go-wm run --once examples/scripts/hello.js
//
// print() is not console.log: each argument becomes a *segment* in the
// desktop listener, and object segments render as live presentations —
// the color below is clickable, right-clickable, mixable in any listener
// tile, because it is an object, not text.

const pbui = require("pbui");

pbui.print(
  "hello from JavaScript — this is ",
  pbui.object("color", "#b0563f"),
  " and it is a real object, not a string"
);
