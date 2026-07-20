// rc-tile.js — a WM-embedded scripted tile. [works: GGWM-003, rc.js only]
//
//   go-go-wm wm --display :1 --embedded-broker --rc examples/scripts/rc-tile.js
//
// app.tile() registers the surface with the WM itself: the WM paints it
// like a builtin (trace, listener, …), and clicks route back into this
// runtime. The returned string is a normal app name — place the tile
// with the same ops as everything else.

const ui = require("ui");
const wm = require("wm");
const pbui = require("pbui");

const state = { count: 0, last: "never" };

const app = ui.app({
  name: "js-counter",
  title: "JS COUNTER",
  render() {
    return [
      ui.row(ui.text("COUNTER TILE", { bold: true, size: 13 })),
      ui.row(ui.hint("this surface is rendered by the WM, defined in rc.js")),
      ui.row(ui.text("count: " + state.count, { size: 12 })),
      ui.row(
        ui.button("+1", "inc", { color: "mint" }),
        ui.button("reset", "reset", { color: "rose" }),
      ),
      ui.row(ui.object("number", state.count, { doc: "the count, as a presentation" })),
      ui.row(ui.hint("last change: " + state.last)),
    ];
  },
  actions: {
    inc() { state.count++; state.last = "inc"; },
    reset() { state.count = 0; state.last = "reset"; },
  },
});

// Register the renderer, then put the tile on screen next to whatever
// leaf exists (the same split op a keybinding would issue).
const tileApp = app.tile();
const first = wm.leaves()[0].id;
const leaf = wm.split(first, "row", { app: tileApp });

pbui.print("js-counter tile placed at ", pbui.object("tile", leaf));
