// net-verbs.js — desktop-wide verbs for ip and url objects. [works today]
//
//   go-go-wm run examples/scripts/net-verbs.js          # daemon
//
// After this loads, every ip or url presentation anywhere on the desktop
// — an address scraped out of a kitty terminal (dig, ip a, ss, a log line
// piped through `go-go-wm scrape`), a url in `git log` output — gains
// these entries in its right-click menu. The handlers run in THIS
// process; the broker routes the invocation from wherever you clicked.
//
// Every action here prints back into the WM listener (a live presentation
// you can click again), so nothing depends on spawning subprocesses —
// the point is that a verb registered once serves the whole desktop.

const pbui = require("pbui");

pbui.verb(
  { id: "ip.octets", label: "Show octets", ptypes: ["ip"] },
  (o) => {
    const parts = String(o.value).split(".");
    pbui.print("ip ", pbui.object("ip", o.value), " → ", parts.join(" · "));
  }
);

pbui.verb(
  { id: "ip.reverse", label: "Reverse (for PTR)", ptypes: ["ip"] },
  (o) => {
    const r = String(o.value).split(".").reverse().join(".");
    pbui.print("reversed ", pbui.object("ip", r), "  (", r + ".in-addr.arpa", ")");
  }
);

pbui.verb(
  { id: "ip.private", label: "Public or private?", ptypes: ["ip"] },
  (o) => {
    const [a, b] = String(o.value).split(".").map(Number);
    const priv =
      a === 10 ||
      (a === 172 && b >= 16 && b <= 31) ||
      (a === 192 && b === 168) ||
      a === 127;
    pbui.print(pbui.object("ip", o.value), priv ? " is PRIVATE / loopback" : " is PUBLIC");
  }
);

pbui.verb(
  { id: "url.host", label: "Extract host", ptypes: ["url"] },
  (o) => {
    try {
      const u = new URL(o.value);
      pbui.print("host ", pbui.object("string", u.host), "  path ", pbui.object("string", u.pathname || "/"));
    } catch (e) {
      pbui.print("not a parseable url: ", pbui.object("url", o.value));
    }
  }
);

pbui.print("net-verbs loaded: ip objects got 3 verbs, url objects got 1");
