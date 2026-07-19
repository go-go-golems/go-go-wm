// palette.js — accept protocol from a script. [works today: P1]
//
//   go-go-wm run --once examples/scripts/palette.js
//
// The script asks the *desktop* for a color: the WM banner switches to
// ACCEPTING, every color presentation on screen starts pulsing, and
// whichever one you click resolves the promise below — even if it lives
// in another process's window. Cancellation (Escape) resolves null.

const pbui = require("pbui");

function shade(hex, f) {
  const n = parseInt(hex.slice(1), 16);
  const ch = (x) => Math.max(0, Math.min(255, Math.round(x * f)));
  const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map(ch);
  return "#" + ((r << 16) | (g << 8) | b).toString(16).padStart(6, "0");
}

async function main() {
  const picked = await pbui.accept("color", "PALETTE — click any color on screen");
  if (picked === null) {
    pbui.print("palette: cancelled");
    return;
  }
  const segs = ["palette of ", pbui.object("color", picked.value), " → "];
  for (const f of [0.4, 0.7, 1.3, 1.6]) {
    segs.push(pbui.object("color", shade(picked.value, f)), " ");
  }
  pbui.print(...segs);
}

main();
