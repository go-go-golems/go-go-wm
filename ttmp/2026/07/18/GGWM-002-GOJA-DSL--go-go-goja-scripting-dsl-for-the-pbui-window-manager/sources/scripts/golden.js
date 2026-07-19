// golden.js — reshape the desktop, then assert the result. [P2: wm module]
//
//   go-go-wm run --once examples/scripts/golden.js
//
// The wm module compiles every call here into the same serializable Ops
// the keyboard and mouse produce, applies them over the control socket,
// and reads the layout tree back. A script that asserts its own
// postconditions and exits non-zero on failure IS the integration test.

const wm = require("wm");

function assert(cond, msg) {
  if (!cond) throw new Error("golden.js: " + msg);
}

const before = wm.tree();
const ws = before.current;

// Build:  editor | (terminal / notes)
const first = wm.focused() || before.workspaces[0].leaves[0].id;
const right = wm.split(first, "row", { ratio: 0.6 });
const bottom = wm.split(right, "col");
wm.setApp(first, "editor");
wm.setApp(right, "terminal");
wm.setApp(bottom, "notes");

const after = wm.tree();
const leaves = wm.leaves(after.current);
assert(leaves.length >= 3, "expected at least 3 leaves, got " + leaves.length);
assert(leaves.some((l) => l.app === "notes"), "notes leaf missing");

require("pbui").print("golden.js: layout verified — ", String(leaves.length), " tiles");
