// golden.js — reshape the desktop, then assert the result. [works: P2]
//
//   go-go-wm run --once examples/scripts/golden.js
//
// The wm module compiles every call here into the same serializable Ops
// the keyboard and mouse produce, applies them over the control socket,
// and reads the layout tree back. A script that asserts its own
// postconditions and exits non-zero on failure IS the integration test.

const wm = require("wm");
const pbui = require("pbui");

function assert(cond, msg) {
  if (!cond) throw new Error("golden.js: " + msg);
}

// Start from whatever tile is focused (or the first one).
const start = wm.focused() || wm.leaves()[0].id;

// Build:  editor | (terminal / notes)
const right = wm.split(start, "row", { ratio: 0.62, app: "builtin:trace" });
const bottom = wm.split(right, "col", { app: "builtin:listener" });

const leaves = wm.leaves();
assert(leaves.length >= 3, "expected at least 3 leaves, got " + leaves.length);
assert(leaves.some((l) => l.app === "builtin:trace"), "trace leaf missing");
assert(leaves.some((l) => l.app === "builtin:listener"), "listener leaf missing");

// The ratio must have landed on the parent split of the trace leaf.
const d = wm.tree();
const ws = d.workspaces.find((w) => w.id === d.current);
let ratioOK = false;
(function walk(n) {
  if (!n) return;
  if (n.kind === "split" && Math.abs(n.ratio - 0.62) < 1e-9) ratioOK = true;
  walk(n.a); walk(n.b);
})(ws.root);
assert(ratioOK, "no split with ratio 0.62 found");

pbui.print("golden.js: layout verified — ", String(leaves.length), " tiles ✓");
