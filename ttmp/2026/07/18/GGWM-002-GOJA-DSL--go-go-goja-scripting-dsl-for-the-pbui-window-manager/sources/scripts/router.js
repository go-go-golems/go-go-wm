// router.js — an event-driven window butler. [works: P2]
//
//   go-go-wm run examples/scripts/router.js             # daemon
//
// Subscribes to the WM's own event stream (every op is an event, because
// every op is data) and reacts: new windows whose title matches a pattern
// get adopted by the workspace where they belong.

const wm = require("wm");
const pbui = require("pbui");

const ROUTES = [
  { match: /firefox|mozilla/i, workspace: "web" },
  { match: /gimp|inkscape/i, workspace: "art" },
];

wm.on("window.managed", (ev) => {
  const title = ev.data.title || "";
  for (const r of ROUTES) {
    if (!r.match.test(title)) continue;
    wm.workspace(r.workspace).adopt(ev.data.leaf);
    pbui.print("routed ", title, " → workspace ", r.workspace);
    return;
  }
});

pbui.print("router.js armed: firefox→web, gimp→art");
