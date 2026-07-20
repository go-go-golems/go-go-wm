// js-colors.js — a full PBUI app written in JavaScript. [works: GGWM-003]
//
//   go-go-wm run examples/scripts/js-colors.js          # daemon
//
// ui.app turns a render() function into a real desktop citizen: an X
// window the WM tiles, whose color chips pulse during accepts, answer
// clicks, and carry this app's own verb in their right-click menu.
// State lives in plain JS; every handler triggers a re-render.

const ui = require("ui");
const pbui = require("pbui");

const state = {
  colors: ["#b0563f", "#5a7a58", "#8a7ca8"],
};

function randColor() {
  const c = () => Math.floor(64 + Math.random() * 160).toString(16).padStart(2, "0");
  return "#" + c() + c() + c();
}

const app = ui.app({
  name: "js-colors",
  title: "JS COLORS",
  render() {
    return [
      ui.row(ui.text("A JavaScript PBUI app", { bold: true, size: 13 })),
      ui.row(ui.hint("chips are <color> presentations — click during accepts, right-click for verbs")),
      ui.row(...state.colors.map((c) => ui.object("color", c))),
      ui.row(
        ui.button("Add random", "add", { color: "rose" }),
        ui.button("Drop last", "drop", { color: "blue" }),
        ui.button("Steal one…", "steal", { color: "mint", doc: "accept a color from anywhere" }),
      ),
      ui.row(ui.hint(String(state.colors.length) + " colors")),
    ];
  },
  actions: {
    add() { state.colors.push(randColor()); },
    drop() { state.colors.pop(); },
    async steal() {
      const got = await pbui.accept("color", "STEAL — click any color anywhere");
      if (got !== null) state.colors.push(got.value);
      app.refresh();
    },
  },
  verbs: [
    { id: "color.darken", label: "Darken (js-colors)", ptypes: ["color"],
      run(obj) {
        const n = parseInt(obj.value.slice(1), 16);
        const d = (x) => Math.max(0, Math.floor(x * 0.6));
        const hex = ((d((n >> 16) & 255) << 16) | (d((n >> 8) & 255) << 8) | d(n & 255))
          .toString(16).padStart(6, "0");
        state.colors.push("#" + hex);
      } },
  ],
});

app.show();
pbui.print("js-colors is up — a JS app serving chips, verbs, and accepts");
