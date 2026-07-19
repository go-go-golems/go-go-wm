// router.js — an event-driven window butler. [P2: wm module]
//
//   go-go-wm run examples/scripts/router.js             # daemon
//
// Subscribes to the WM's own event stream (every op is an event, because
// every op is data) and reacts: new windows whose app name matches a
// pattern get moved to the workspace where they belong.

const wm = require("wm");
const pbui = require("pbui");

const ROUTES = [
  { match: /firefox|chromium/i, workspace: "web" },
  { match: /gimp|inkscape/i, workspace: "art" },
];

wm.on("window.managed", (ev) => {
  const app = ev.data.app || "";
  for (const r of ROUTES) {
    if (!r.match.test(app)) continue;
    wm.workspace(r.workspace).adopt(ev.data.leaf);
    pbui.print("routed ", app, " → workspace ", r.workspace);
    return;
  }
});

pbui.print("router.js armed: firefox→web, gimp→art");
