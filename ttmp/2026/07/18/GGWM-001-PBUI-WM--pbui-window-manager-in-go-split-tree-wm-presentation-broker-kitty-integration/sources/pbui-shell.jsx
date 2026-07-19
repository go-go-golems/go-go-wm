import React, { useState, useRef, useEffect, useCallback, useContext } from "react";

/* ============================================================
   PBUI SHELL — Prototype 1.0
   A CLIM / Genera "Dynamic Windows" flavored framework:

   PRESENTATION LAYER (the framework)
     · every rendered object is a typed, live presentation
     · accept protocol: commands read OBJECTS, not strings —
       from ANY tile, in ANY workspace, mid-command
     · right-click object menus, type-directed actions
     · mouse-documentation line (bottom), accept banner (top)

   WINDOW MANAGER (the shell)
     · binary split tree: split ⇄ / ⇅, close, resize
     · Blender-style sticky zones: borders snap at ¼ ⅓ ½ ⅔ ¾
     · drag ⠿ to move an app onto another tile (swap)
     · tiles + workspaces are THEMSELVES presentations
     · workspaces: add / rename / duplicate / delete

   APPS
     · singletons: two tiles on the same app are two live
       views of ONE state (Genera-style)
     · registered in APPS{} — a component + a title + a color.
       Port anything (e.g. the SGEP machine panes) by adding
       an entry and speaking <P ptype=… value=…>.
   ============================================================ */

/* ---------------- palette ---------------- */
const C = {
  paper: "#e9e2d0", pane: "#f5f0e3", paneAlt: "#efe9d9",
  ink: "#33302a", faint: "#7a7365",
  sage: "#a9bda2", blue: "#9cb4c2", rose: "#cfa08f",
  mustard: "#d3b56a", lavender: "#b3abc4", mint: "#b7c9b3",
  red: "#b0563f", sel: "#f4e6b8",
};
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));

/* ============================================================
   PBUI CORE — presentations + accept
   ============================================================ */
const UICtx = React.createContext(null);
const useUI = () => useContext(UICtx);
const typeMatches = (want, have) =>
  want === "any" || (Array.isArray(want) ? want.includes(have) : want === have);

/* P — present a value under a presentation type.
   L-click: accept (when a matching accept() is pending)
            → else onActivate (if the presentation has a primary action)
            → else the object menu (same as right-click).
   R-click: always the object menu.  Hover: mouse-doc line. */
function P({ ptype, value, doc, children, block, onActivate, activateDoc }) {
  const ui = useUI();
  const acceptable = ui.accepting && typeMatches(ui.accepting.ptype, ptype);
  const Tag = block ? "div" : "span";
  const clickDoc = acceptable ? "L: ACCEPT   R: menu"
    : onActivate ? "L: " + (activateDoc || "activate") + "   R: menu"
    : "L/R: menu";
  return (
    <Tag
      className={"pres" + (acceptable ? " acceptable" : "")}
      onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); ui.openMenu(ptype, value, e.clientX, e.clientY); }}
      onClick={(e) => {
        e.stopPropagation();
        if (acceptable) { e.preventDefault(); ui.accepting.resolve({ ptype, value }); ui.setAccepting(null); }
        else if (onActivate) onActivate();
        else ui.openMenu(ptype, value, e.clientX, e.clientY);
      }}
      onMouseEnter={() => ui.setMouseDoc((doc || "<" + ptype + "> " + String(value).slice(0, 24)) + "   —   " + clickDoc)}
      onMouseLeave={() => ui.setMouseDoc(null)}
    >{children}</Tag>
  );
}

/* Pres — a default visual for a (ptype, value), used when an app
   re-presents an object it did not originate (notes, listener). */
function Pres({ ptype, value }) {
  const ui = useUI();
  let inner;
  if (ptype === "color") inner = (<>
    <span style={{ display: "inline-block", width: 11, height: 11, background: value, border: "1px solid " + C.ink, marginRight: 4, verticalAlign: "-1px" }} />{value}</>);
  else if (ptype === "number") inner = <b>{String(value)}</b>;
  else inner = <span>{ui.labelFor(ptype, value)}</span>;
  return (
    <P ptype={ptype} value={value}>
      <span style={{ background: C.paneAlt, border: "1px solid " + C.ink, padding: "0 4px", fontSize: 11 }}>{inner}</span>
    </P>
  );
}

/* ============================================================
   WORLD — shared singleton state for all apps
   ============================================================ */
let seqc = 0, notec = 0;
class World {
  constructor() {
    this.notify = null;
    this.trace = [];
    this.notes = [];
    this.colors = ["#b0563f", "#d3b56a", "#9cb4c2", "#a9bda2", "#b3abc4", "#cfa08f"];
    this.listener = [{ segs: [{ text: "PBUI listener. Commands here accept objects from ANY tile in ANY workspace." }] }];
    this.inspected = {
      title: "about this shell",
      value: {
        framework: "presentations + accept + object menus (CLIM-style)",
        windowManager: "split tree · sticky snap zones · drag ⠿ to move apps · workspaces",
        apps: "singletons — two tiles on one app are two views of one state",
        try: "open the 'about / help' app, or just click anything",
      },
    };
  }
  bump() { this.notify && this.notify(); }
  log(type, data) { this.trace.push({ seq: ++seqc, type, data: data || {} }); this.bump(); }
  print(segs) { this.listener.push({ segs }); this.bump(); }
  inspect(title, value) { this.inspected = { title, value }; this.log("inspected", { title }); }
  collect(ptype, value) {
    this.notes.push({ id: ++notec, ptype, value });
    this.log("collected", { ptype, value: String(value).slice(0, 24), note: "collected object stays LIVE — it can still be accepted and menued from the notes tile" });
  }
  removeNote(id) { this.notes = this.notes.filter((n) => n.id !== id); this.log("note_removed", { id }); }
  addColor(hex, note) { if (!this.colors.includes(hex)) this.colors.push(hex); this.log("color_added", { hex, note }); }
  removeColor(hex) { this.colors = this.colors.filter((c) => c !== hex); this.log("color_removed", { hex }); }
}

/* ---------------- little value helpers ---------------- */
const hexToRgb = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
const rgbToHex = (r) => "#" + r.map((x) => clamp(Math.round(x), 0, 255).toString(16).padStart(2, "0")).join("");
const mixHex = (a, b) => rgbToHex(hexToRgb(a).map((x, i) => (x + hexToRgb(b)[i]) / 2));
const isPrime = (n) => { if (n < 2) return false; for (let i = 2; i * i <= n; i++) if (n % i === 0) return false; return true; };
const factorize = (n) => { const f = []; let m = n; for (let d = 2; d * d <= m; d++) while (m % d === 0) { f.push(d); m /= d; } if (m > 1) f.push(m); return f; };

/* ============================================================
   WINDOW MANAGER — split tree + workspaces
   ============================================================ */
let idc = 1;
const nid = () => "n" + idc++;
const leaf = (app) => ({ id: nid(), type: "leaf", app });
const split = (dir, a, b, ratio = 0.5) => ({ id: nid(), type: "split", dir, a, b, ratio });

function updateNode(node, id, fn) {
  if (node.id === id) return fn(node);
  if (node.type === "split") {
    const a = updateNode(node.a, id, fn), b = updateNode(node.b, id, fn);
    if (a !== node.a || b !== node.b) return { ...node, a, b };
  }
  return node;
}
function removeLeaf(node, id) {
  if (node.type === "split") {
    if (node.a.id === id) return node.b;
    if (node.b.id === id) return node.a;
    const a = removeLeaf(node.a, id), b = removeLeaf(node.b, id);
    if (a !== node.a || b !== node.b) return { ...node, a, b };
  }
  return node;
}
function findLeaf(node, id) {
  if (node.type === "leaf") return node.id === id ? node : null;
  return findLeaf(node.a, id) || findLeaf(node.b, id);
}
function countLeaves(node) { return node.type === "leaf" ? 1 : countLeaves(node.a) + countLeaves(node.b); }
function cloneTree(node) {
  return node.type === "leaf" ? { ...node, id: nid() } : { ...node, id: nid(), a: cloneTree(node.a), b: cloneTree(node.b) };
}

/* sticky zones (Blender-style): dividers snap to these fractions */
const SNAPS = [0.25, 1 / 3, 0.5, 2 / 3, 0.75];
const STICK = 0.022;
function snapFrac(f) { for (const s of SNAPS) if (Math.abs(f - s) < STICK) return { f: s, snapped: true }; return { f, snapped: false }; }

function WMDivider({ dir, containerRef, onRatio }) {
  const [mode, setMode] = useState(0); // 0 idle · 1 hot · 2 dragging · 3 snapped
  const row = dir === "row";
  const down = (e) => {
    e.preventDefault();
    const prev = document.body.style.userSelect; document.body.style.userSelect = "none";
    const move = (ev) => {
      const el = containerRef.current; if (!el) return;
      const r = el.getBoundingClientRect();
      let f = row ? (ev.clientX - r.left) / r.width : (ev.clientY - r.top) / r.height;
      f = clamp(f, 0.1, 0.9);
      const s = snapFrac(f);
      setMode(s.snapped ? 3 : 2);
      onRatio(s.f);
    };
    const up = () => { document.body.style.userSelect = prev; setMode(0); window.removeEventListener("mousemove", move); window.removeEventListener("mouseup", up); };
    window.addEventListener("mousemove", move); window.addEventListener("mouseup", up);
  };
  const size = row ? { width: 8, cursor: "col-resize", alignSelf: "stretch" } : { height: 8, cursor: "row-resize" };
  return (
    <div onMouseDown={down} onMouseEnter={() => mode === 0 && setMode(1)} onMouseLeave={() => mode === 1 && setMode(0)}
      style={{ ...size, flexShrink: 0, background: mode === 3 ? C.mustard : mode === 2 ? C.sage : mode === 1 ? C.paneAlt : "transparent", display: "flex", alignItems: "center", justifyContent: "center" }}>
      <div style={row ? { width: 2, height: 26, borderLeft: "2px dotted " + C.faint } : { height: 2, width: 26, borderTop: "2px dotted " + C.faint }} />
    </div>
  );
}

function NodeView({ node }) {
  return node.type === "leaf" ? <TileView leafNode={node} /> : <SplitView node={node} />;
}
function SplitView({ node }) {
  const ui = useUI();
  const ref = useRef(null);
  const row = node.dir === "row";
  return (
    <div ref={ref} style={{ flex: 1, display: "flex", flexDirection: row ? "row" : "column", minWidth: 0, minHeight: 0, alignItems: "stretch" }}>
      <div style={{ flex: node.ratio + " 1 0px", display: "flex", minWidth: 0, minHeight: 0 }}><NodeView node={node.a} /></div>
      <WMDivider dir={node.dir} containerRef={ref} onRatio={(r) => ui.wm.setRatio(node.id, r)} />
      <div style={{ flex: (1 - node.ratio) + " 1 0px", display: "flex", minWidth: 0, minHeight: 0 }}><NodeView node={node.b} /></div>
    </div>
  );
}

function TBtn({ onClick, children, doc, disabled }) {
  const ui = useUI();
  return (
    <span
      onMouseEnter={() => ui.setMouseDoc(doc)} onMouseLeave={() => ui.setMouseDoc(null)}
      onClick={disabled ? undefined : (e) => { e.stopPropagation(); onClick(); }}
      style={{ cursor: disabled ? "default" : "pointer", opacity: disabled ? 0.35 : 1, border: "1px solid " + C.ink, background: C.paneAlt, padding: "0 5px", fontSize: 10, fontWeight: 700, userSelect: "none", lineHeight: "15px" }}
    >{children}</span>
  );
}

function TileView({ leafNode }) {
  const ui = useUI();
  const app = APPS[leafNode.app];
  const Comp = app.comp;
  const drag = ui.drag;
  const isTarget = drag && drag.over === leafNode.id && drag.from !== leafNode.id;
  const isSource = drag && drag.from === leafNode.id;
  const zone = isTarget ? drag.zone : null;
  const zoneRect =
    zone === "left" ? { left: 0, top: 0, bottom: 0, width: "50%" } :
    zone === "right" ? { right: 0, top: 0, bottom: 0, width: "50%" } :
    zone === "top" ? { top: 0, left: 0, right: 0, height: "50%" } :
    zone === "bottom" ? { bottom: 0, left: 0, right: 0, height: "50%" } :
    zone === "center" ? { inset: 0 } : null;
  return (
    <div ref={(el) => ui.wm.registerRef(leafNode.id, el)} style={{
      flex: 1, display: "flex", flexDirection: "column", border: "2px solid " + C.ink, background: C.pane,
      minWidth: 0, minHeight: 0, position: "relative", opacity: isSource ? 0.75 : 1,
    }}>
      {zoneRect && (
        <div style={{
          position: "absolute", ...zoneRect, zIndex: 5, pointerEvents: "none",
          background: "rgba(176,86,63,0.22)", border: "3px dashed " + C.red,
          display: "flex", alignItems: "center", justifyContent: "center",
        }}>
          <span style={{ background: C.pane, border: "2px solid " + C.ink, boxShadow: "2px 2px 0 " + C.ink, padding: "1px 8px", fontSize: 10.5, fontWeight: 700 }}>
            {zone === "center" ? "⇄ swap apps" : "split-dock here · old tile closes"}
          </span>
        </div>
      )}
      <div style={{ display: "flex", alignItems: "center", gap: 6, background: app.color, borderBottom: "2px solid " + C.ink, padding: "2px 6px", flexShrink: 0 }}>
        <span
          onMouseDown={(e) => ui.wm.startDrag(leafNode.id, e)}
          onMouseEnter={() => ui.setMouseDoc("drag ⠿ — drop on a tile's CENTER to swap apps, or near an EDGE to split-dock there (this tile then closes)")} onMouseLeave={() => ui.setMouseDoc(null)}
          style={{ cursor: "grab", fontWeight: 700, userSelect: "none" }}>⠿</span>
        <P ptype="tile" value={leafNode.id} doc={"tile [" + app.title + "] — split / close / swap"}>
          <b style={{ fontSize: 11, letterSpacing: "0.05em", textTransform: "uppercase" }}>{app.title}</b>
        </P>
        <span style={{ flex: 1 }} />
        <select value={leafNode.app} onChange={(e) => ui.wm.setLeafApp(leafNode.id, e.target.value)}
          onMouseDown={(e) => e.stopPropagation()}
          style={{ border: "1px solid " + C.ink, background: C.pane, fontSize: 10, padding: "0 2px", fontFamily: "inherit" }}>
          {Object.entries(APPS).map(([id, a]) => <option key={id} value={id}>{a.title}</option>)}
        </select>
        <TBtn doc="split this tile: new tile to the RIGHT" onClick={() => ui.wm.splitLeaf(leafNode.id, "row")}>⬌</TBtn>
        <TBtn doc="split this tile: new tile BELOW" onClick={() => ui.wm.splitLeaf(leafNode.id, "col")}>⬍</TBtn>
        <TBtn doc="close this tile (its sibling absorbs the space)" disabled={!ui.wm.canClose} onClick={() => ui.wm.closeLeaf(leafNode.id)}>✕</TBtn>
      </div>
      <div style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
        <Comp leafId={leafNode.id} />
      </div>
    </div>
  );
}

/* ============================================================
   DEMO APPLICATIONS
   ============================================================ */
const AppBody = ({ children, style }) => (
  <div style={{ flex: 1, minHeight: 0, overflow: "auto", padding: "6px 8px", ...style }}>{children}</div>
);
const Hint = ({ children }) => <div style={{ color: C.faint, fontSize: 10.5, marginBottom: 6 }}>{children}</div>;
function Btn({ onClick, children, tone, disabled, title }) {
  return (
    <button title={title} disabled={disabled} onClick={onClick} style={{
      fontFamily: "inherit", fontSize: 11, fontWeight: 700, letterSpacing: "0.04em",
      background: disabled ? C.paneAlt : (tone || C.blue), color: C.ink, border: "2px solid " + C.ink,
      boxShadow: "2px 2px 0 " + C.ink, padding: "3px 10px", cursor: disabled ? "default" : "pointer", opacity: disabled ? 0.5 : 1,
    }}>{children}</button>
  );
}

function LauncherApp({ leafId }) {
  const ui = useUI();
  return (
    <AppBody>
      <Hint>empty tile — choose an application (apps are singletons: opening one twice shows the same live state)</Hint>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
        {Object.entries(APPS).filter(([id]) => id !== "launcher").map(([id, a]) => (
          <Btn key={id} tone={a.color} onClick={() => ui.wm.setLeafApp(leafId, id)}>{a.title}</Btn>
        ))}
      </div>
    </AppBody>
  );
}

function ColorsApp() {
  const ui = useUI();
  const w = ui.world;
  return (
    <AppBody>
      <Hint>swatches are &lt;color&gt; presentations · click one → “Mix with…” then click ANY color anywhere (other tiles, notes, listener output)</Hint>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 8 }}>
        {w.colors.map((hex) => (
          <P key={hex} ptype="color" value={hex} doc={"color " + hex} block>
            <div style={{ border: "2px solid " + C.ink, background: C.paneAlt, padding: 3 }}>
              <div style={{ width: 46, height: 30, background: hex, border: "1px solid " + C.ink }} />
              <div style={{ fontSize: 9.5, textAlign: "center" }}>{hex}</div>
            </div>
          </P>
        ))}
      </div>
      <Btn tone={C.rose} onClick={() => {
        const hex = rgbToHex([0, 0, 0].map(() => 80 + Math.random() * 150));
        w.addColor(hex, "random");
      }}>Add random swatch</Btn>
    </AppBody>
  );
}

function NumbersApp() {
  return (
    <AppBody>
      <Hint>a field of &lt;number&gt; presentations (primes shaded) · click → factorize, multiply-with…, collect · the listener's Sum… command can pick them up</Hint>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 3 }}>
        {Array.from({ length: 60 }, (_, i) => i + 2).map((n) => (
          <P key={n} ptype="number" value={n} doc={"number " + n + (isPrime(n) ? " (prime)" : "")}>
            <span style={{ display: "inline-block", minWidth: 26, textAlign: "center", border: "1px solid " + C.ink, background: isPrime(n) ? C.sage : C.paneAlt, padding: "1px 2px", fontSize: 11, fontWeight: isPrime(n) ? 700 : 400 }}>{n}</span>
          </P>
        ))}
      </div>
    </AppBody>
  );
}

function NotesApp() {
  const ui = useUI();
  const w = ui.world;
  return (
    <AppBody>
      <div style={{ marginBottom: 6 }}>
        <Btn tone={C.mustard} onClick={async () => {
          const r = await ui.accept("any", "Click ANY presentation — any tile, any workspace — to collect it (Esc cancels)");
          if (r) w.collect(r.ptype, r.value);
        }}>Collect… (accept anything)</Btn>
      </div>
      <Hint>collected objects remain LIVE presentations: a collected color still mixes, a collected number still sums</Hint>
      {w.notes.map((n) => (
        <div key={n.id} style={{ display: "flex", gap: 6, alignItems: "baseline", marginBottom: 3 }}>
          <P ptype="note" value={n.id} doc={"note #" + n.id + " — remove / inspect"}>
            <span style={{ color: C.faint, fontSize: 10, textDecoration: "underline dotted" }}>#{n.id}</span>
          </P>
          <span style={{ color: C.faint, fontSize: 10 }}>&lt;{n.ptype}&gt;</span>
          <Pres ptype={n.ptype} value={n.value} />
        </div>
      ))}
      {w.notes.length === 0 && <div style={{ color: C.faint }}>Nothing collected yet.</div>}
    </AppBody>
  );
}

function ListenerApp() {
  const ui = useUI();
  const w = ui.world;
  const [text, setText] = useState("");
  const endRef = useRef(null);
  useEffect(() => { endRef.current && endRef.current.scrollIntoView({ block: "nearest" }); }, [w.listener.length]);

  const describeCmd = async () => {
    const r = await ui.accept("any", "DESCRIBE — click any presentation anywhere (Esc cancels)");
    if (!r) return;
    w.inspect("<" + r.ptype + "> " + ui.labelFor(r.ptype, r.value), ui.describe(r.ptype, r.value));
    w.print([{ text: "described " }, { ptype: r.ptype, value: r.value }, { text: " → see inspector" }]);
  };
  const sumCmd = async () => {
    const a = await ui.accept("number", "SUM — click a NUMBER (1 of 2) — numbers app, notes, prior results all work");
    if (!a) return;
    const b = await ui.accept("number", "SUM — click a NUMBER (2 of 2)");
    if (!b) return;
    const r = a.value + b.value;
    w.print([{ text: a.value + " + " + b.value + " = " }, { ptype: "number", value: r }]);
    w.log("sum", { a: a.value, b: b.value, result: r, note: "result is itself a live <number> presentation" });
  };
  const colorCmd = async () => {
    const r = await ui.accept("color", "PICK — click a COLOR swatch anywhere (Esc cancels)");
    if (!r) return;
    w.print([{ text: "picked " }, { ptype: "color", value: r.value }, { text: " luminance " + Math.round(hexToRgb(r.value).reduce((s, x) => s + x, 0) / 7.65) / 100 }]);
  };
  const submit = () => {
    const t = text.trim(); if (!t) return;
    const m = t.match(/^(\d+)\s*([+\-*])\s*(\d+)$/);
    if (m) {
      const a = +m[1], b = +m[3];
      const r = m[2] === "+" ? a + b : m[2] === "-" ? a - b : a * b;
      w.print([{ text: "> " + t + " ⇒ " }, { ptype: "number", value: r }]);
    } else w.print([{ text: "> " + t + "   (tip: the buttons above read objects, not strings)" }]);
    setText("");
  };

  return (
    <>
      <div style={{ display: "flex", gap: 6, flexWrap: "wrap", padding: "6px 8px 0" }}>
        <Btn tone={C.blue} onClick={describeCmd}>Describe…</Btn>
        <Btn tone={C.sage} onClick={sumCmd}>Sum…</Btn>
        <Btn tone={C.rose} onClick={colorCmd}>Pick color…</Btn>
      </div>
      <div style={{ flex: 1, minHeight: 0, overflow: "auto", padding: "6px 8px" }}>
        {w.listener.map((line, i) => (
          <div key={i} style={{ marginBottom: 2, fontSize: 11.5, wordBreak: "break-word" }}>
            {line.segs.map((s, j) => s.ptype !== undefined
              ? <Pres key={j} ptype={s.ptype} value={s.value} />
              : <span key={j}>{s.text}</span>)}
          </div>
        ))}
        <div ref={endRef} />
      </div>
      <div style={{ display: "flex", gap: 6, padding: "0 8px 6px", flexShrink: 0 }}>
        <input value={text} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => e.key === "Enter" && submit()}
          placeholder="type 3+4, or use the object-reading commands above"
          style={{ flex: 1, border: "2px solid " + C.ink, background: "#fbf8ef", padding: "3px 6px", fontSize: 11, fontFamily: "inherit", minWidth: 0 }} />
        <Btn tone={C.mint} onClick={submit}>eval</Btn>
      </div>
    </>
  );
}

function InspectorApp() {
  const w = useUI().world;
  return (
    <AppBody>
      <div style={{ fontWeight: 700, marginBottom: 4, borderBottom: "1px dashed " + C.faint }}>{w.inspected.title}</div>
      <pre style={{ margin: 0, fontSize: 10.5, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{JSON.stringify(w.inspected.value, null, 2)}</pre>
    </AppBody>
  );
}

const EV_COLOR = {
  accepted: C.mustard, collected: C.sage, note_removed: C.rose,
  color_added: C.rose, color_removed: C.rose, mixed: C.rose, sum: C.blue, product: C.blue,
  split_tile: C.lavender, close_tile: C.lavender, swap_tiles: C.lavender, move_split: C.lavender, app_changed: C.lavender,
  workspace_added: C.mint, workspace_removed: C.rose, workspace_renamed: C.mint, workspace_cloned: C.mint,
  inspected: C.paneAlt,
};
function TraceApp() {
  const w = useUI().world;
  const endRef = useRef(null);
  useEffect(() => { endRef.current && endRef.current.scrollIntoView({ block: "nearest" }); }, [w.trace.length]);
  return (
    <AppBody>
      {w.trace.map((e) => (
        <div key={e.seq} style={{ display: "flex", gap: 6, alignItems: "baseline", marginBottom: 1 }}>
          <span style={{ color: C.faint, fontSize: 10, width: 26, textAlign: "right", flexShrink: 0 }}>{e.seq}</span>
          <P ptype="event" value={e.seq} doc={"event #" + e.seq + " " + e.type}>
            <span style={{ background: EV_COLOR[e.type] || C.paneAlt, border: "1px solid " + C.ink, padding: "0 4px", fontSize: 9.5, fontWeight: 700 }}>{e.type}</span>
          </P>
          <span style={{ fontSize: 10.5, wordBreak: "break-word" }}>
            {Object.entries(e.data).filter(([k]) => k !== "note").map(([k, v]) => <span key={k}>{k}={String(v)} </span>)}
            {e.data.note && <span style={{ color: C.faint }}>· {e.data.note}</span>}
          </span>
        </div>
      ))}
      {w.trace.length === 0 && <div style={{ color: C.faint }}>Nothing yet — do something.</div>}
      <div ref={endRef} />
    </AppBody>
  );
}

function AboutApp() {
  const Sec = ({ t, children }) => (
    <div style={{ marginBottom: 10 }}>
      <div style={{ fontWeight: 700, letterSpacing: "0.08em", textTransform: "uppercase", fontSize: 10.5, background: C.sel, display: "inline-block", padding: "0 6px", border: "1px solid " + C.ink }}>{t}</div>
      <div style={{ marginTop: 4, fontSize: 11.5 }}>{children}</div>
    </div>
  );
  return (
    <AppBody>
      <Sec t="what this is">
        A prototype shell for presentation-based UIs (CLIM / Genera Dynamic Windows lineage).
        Every visible object is a typed <b>presentation</b>; commands read <b>objects</b> via the
        accept protocol; the shell composes independent apps into resizable tiles and workspaces.
      </Sec>
      <Sec t="the composability point">
        Accept works <b>across app boundaries</b>: “Mix with…” in the color lab will happily take a
        color you collected into notes, or one printed by the listener. Apps never talk to each
        other — they only speak presentation types. That is the whole framework.
      </Sec>
      <Sec t="things to try">
        <div>· Listener → <b>Sum…</b>, then click one number in the number field and one inside a note.</div>
        <div>· Click a color → <b>Mix with…</b> → click a swatch in a different tile (or a listener chip).</div>
        <div>· Notes → <b>Collect…</b>, then switch workspace mid-accept and click something there.</div>
        <div>· Drag a border: it sticks at ¼ ⅓ ½ ⅔ ¾ (Blender-style zones — the bar flashes mustard when snapped).</div>
        <div>· Drag <b>⠿</b> onto another tile's <b>center</b>: the apps swap.</div>
        <div>· Drag <b>⠿</b> near another tile's <b>edge</b>: a dock preview appears — drop to split that tile there, and the tile you dragged from closes (its sibling reclaims the space).</div>
        <div>· Open the same app in two tiles: one live state, two views (apps are singletons).</div>
        <div>· Click a tile title, or right-click a workspace chip (left click switches) — the shell itself is made of presentations.</div>
      </Sec>
      <Sec t="writing an app">
        An app is one React component reading shared state, plus an entry in APPS{"{}"}. It renders
        objects with <span style={{ background: C.paneAlt }}>&lt;P ptype value&gt;</span>, reads objects with{" "}
        <span style={{ background: C.paneAlt }}>await ui.accept(type, prompt)</span>, and contributes
        right-click verbs in the central action table. Porting e.g. the SGEP machine means: one app
        per pane, presenting &lt;member&gt; &lt;epoch&gt; &lt;content&gt; — the shell does the rest.
      </Sec>
    </AppBody>
  );
}

const APPS = {
  launcher: { title: "new tile", color: C.paneAlt, comp: LauncherApp },
  about: { title: "about / help", color: C.sel, comp: AboutApp },
  colors: { title: "color lab", color: C.rose, comp: ColorsApp },
  numbers: { title: "number field", color: C.blue, comp: NumbersApp },
  listener: { title: "listener", color: C.mint, comp: ListenerApp },
  notes: { title: "notes / clipboard", color: C.mustard, comp: NotesApp },
  inspector: { title: "inspector", color: C.lavender, comp: InspectorApp },
  trace: { title: "trace", color: C.sage, comp: TraceApp },
};

/* ============================================================
   SHELL
   ============================================================ */
const initialSpaces = () => [
  {
    id: nid(), name: "lab",
    tree: split("row",
      split("col", leaf("colors"), leaf("numbers"), 0.5),
      split("col",
        split("row", leaf("listener"), leaf("notes"), 0.55),
        split("row", leaf("trace"), leaf("inspector"), 0.5),
        0.62),
      1 / 3),
  },
  { id: nid(), name: "help", tree: split("row", leaf("about"), leaf("listener"), 0.55) },
];

export default function App() {
  const [, force] = useState(0);
  const bump = useCallback(() => force((x) => x + 1), []);
  const worldRef = useRef(null);
  if (!worldRef.current) { worldRef.current = new World(); }
  const world = worldRef.current;
  useEffect(() => { world.notify = bump; }, [bump, world]);

  const [spaces, setSpaces] = useState(initialSpaces);
  const [cur, setCur] = useState(() => spaces[0].id);
  const [renaming, setRenaming] = useState(null);
  const [menu, setMenu] = useState(null);
  const [accepting, setAccepting] = useState(null);
  const [mouseDoc, setMouseDoc] = useState(null);
  const [drag, setDrag] = useState(null);
  const dragRef = useRef(null); dragRef.current = drag;
  const leafRefs = useRef({});

  const space = spaces.find((s) => s.id === cur) || spaces[0];
  const tree = space.tree;

  /* ---- tree mutations (current workspace) ---- */
  const mutateTree = (fn) => setSpaces((ss) => ss.map((s) => (s.id === space.id ? { ...s, tree: fn(s.tree) } : s)));
  const setRatio = (id, r) => mutateTree((t) => updateNode(t, id, (n) => ({ ...n, ratio: r })));
  const splitLeaf = (id, dir) => {
    mutateTree((t) => updateNode(t, id, (n) => split(dir, n, leaf("launcher"), 0.5)));
    world.log("split_tile", { dir: dir === "row" ? "⬌" : "⬍" });
  };
  const closeLeaf = (id) => { mutateTree((t) => removeLeaf(t, id)); world.log("close_tile", {}); };
  const setLeafApp = (id, app) => {
    mutateTree((t) => updateNode(t, id, (n) => ({ ...n, app })));
    world.log("app_changed", { app: APPS[app].title });
  };
  const swapTiles = (a, b) => {
    mutateTree((t) => {
      const la = findLeaf(t, a), lb = findLeaf(t, b);
      if (!la || !lb) return t;
      return updateNode(updateNode(t, a, (n) => ({ ...n, app: lb.app })), b, (n) => ({ ...n, app: la.app }));
    });
    world.log("swap_tiles", { note: "apps traded places; their state travelled with them (it lives in the world, not the tile)" });
  };
  /* edge-drop: re-dock the dragged app as a new split of the target.
     The source leaf is detached first, so its old tile "collapses" and
     the sibling reclaims the space — a move, not a copy. */
  const moveSplit = (fromId, targetId, zone) => {
    mutateTree((t) => {
      if (fromId === targetId) return t;
      const src = findLeaf(t, fromId);
      if (!src || !findLeaf(t, targetId)) return t;
      const t2 = removeLeaf(t, fromId);
      if (findLeaf(t2, fromId)) return t; // could not detach (single-leaf tree)
      const dir = zone === "left" || zone === "right" ? "row" : "col";
      const before = zone === "left" || zone === "top";
      return updateNode(t2, targetId, (n) => (before ? split(dir, src, n) : split(dir, n, src)));
    });
    world.log("move_split", { zone, note: "edge-drop re-dock: the app split its landing tile; its old tile closed" });
  };

  /* ---- drag ⠿ to move an app between tiles ---- */
  const registerRef = useCallback((id, el) => { if (el) leafRefs.current[id] = el; else delete leafRefs.current[id]; }, []);
  /* zone: "center" = swap apps · "left/right/top/bottom" = split-dock
     (the dragged app splits the target on that side; its old tile closes) */
  const zoneFor = (r, x, y) => {
    const dl = x - r.left, dr = r.right - x, dt = y - r.top, db = r.bottom - y;
    const band = Math.min(Math.min(r.width, r.height) * 0.3, 110);
    const m = Math.min(dl, dr, dt, db);
    if (m > band) return "center";
    if (m === dl) return "left";
    if (m === dr) return "right";
    if (m === dt) return "top";
    return "bottom";
  };
  const hitLeaf = (x, y) => {
    for (const [id, el] of Object.entries(leafRefs.current)) {
      if (!el || !el.isConnected) continue;
      const r = el.getBoundingClientRect();
      if (x >= r.left && x <= r.right && y >= r.top && y <= r.bottom) return { id, zone: zoneFor(r, x, y) };
    }
    return null;
  };
  const startDrag = (leafId, e) => {
    e.preventDefault();
    document.body.style.userSelect = "none";
    setDrag({ from: leafId, x: e.clientX, y: e.clientY, over: null, zone: null });
  };
  useEffect(() => {
    if (!drag) return;
    const move = (e) => setDrag((d) => {
      if (!d) return d;
      const h = hitLeaf(e.clientX, e.clientY);
      return { ...d, x: e.clientX, y: e.clientY, over: h && h.id, zone: h && h.zone };
    });
    const up = () => {
      const d = dragRef.current;
      document.body.style.userSelect = "";
      if (d && d.over && d.over !== d.from) {
        if (d.zone === "center") swapTiles(d.from, d.over);
        else moveSplit(d.from, d.over, d.zone);
      }
      setDrag(null);
    };
    window.addEventListener("mousemove", move); window.addEventListener("mouseup", up);
    return () => { window.removeEventListener("mousemove", move); window.removeEventListener("mouseup", up); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!drag]);

  /* ---- workspaces ---- */
  const addSpace = () => {
    const s = { id: nid(), name: "ws-" + (spaces.length + 1), tree: leaf("launcher") };
    setSpaces((ss) => [...ss, s]); setCur(s.id);
    world.log("workspace_added", { name: s.name });
  };
  const removeSpace = (id) => {
    if (spaces.length < 2) return;
    setSpaces((ss) => ss.filter((s) => s.id !== id));
    if (cur === id) setCur(spaces.find((s) => s.id !== id).id);
    world.log("workspace_removed", {});
  };
  const cloneSpace = (id) => {
    const s = spaces.find((x) => x.id === id); if (!s) return;
    const c = { id: nid(), name: s.name + "′", tree: cloneTree(s.tree) };
    setSpaces((ss) => [...ss, c]); setCur(c.id);
    world.log("workspace_cloned", { from: s.name });
  };

  /* ---- accept + menus (PBUI plumbing) ---- */
  const accept = (ptype, prompt) => new Promise((resolve) =>
    setAccepting({
      ptype, prompt,
      resolve: (r) => {
        if (r) world.log("accepted", { ptype: r.ptype, value: String(r.value).slice(0, 24) });
        resolve(r);
      },
    }));
  useEffect(() => {
    const esc = (e) => {
      if (e.key === "Escape") { setMenu(null); if (accepting) { accepting.resolve(null); setAccepting(null); } }
    };
    window.addEventListener("keydown", esc); return () => window.removeEventListener("keydown", esc);
  }, [accepting]);

  const labelFor = (ptype, value) => {
    if (ptype === "tile") { const l = findLeaf(tree, value); return l ? "[" + APPS[l.app].title + "]" : "(closed tile)"; }
    if (ptype === "workspace") { const s = spaces.find((x) => x.id === value); return s ? s.name : "?"; }
    if (ptype === "event") { const e = world.trace.find((x) => x.seq === value); return "#" + value + (e ? " " + e.type : ""); }
    if (ptype === "note") return "note #" + value;
    return String(value);
  };
  const describe = (ptype, value) => {
    if (ptype === "color") {
      const rgb = hexToRgb(value);
      return { presentationType: "color", hex: value, rgb, luminance: Math.round(rgb.reduce((s, x) => s + x, 0) / 7.65) / 100 };
    }
    if (ptype === "number") return { presentationType: "number", value, prime: isPrime(value), factors: factorize(value) };
    if (ptype === "note") { const n = world.notes.find((x) => x.id === value); return { presentationType: "note", ...n }; }
    if (ptype === "event") return world.trace.find((x) => x.seq === value) || { seq: value };
    if (ptype === "tile") { const l = findLeaf(tree, value); return { presentationType: "tile", app: l ? APPS[l.app].title : "(closed)", workspace: space.name }; }
    if (ptype === "workspace") { const s = spaces.find((x) => x.id === value); return { presentationType: "workspace", name: s && s.name, tiles: s && countLeaves(s.tree) }; }
    return { presentationType: ptype, value: String(value) };
  };

  /* ---- type-directed action table (right-click verbs) ---- */
  const actionsFor = (ptype, value) => {
    const acts = [{ label: "Inspect", run: () => world.inspect("<" + ptype + "> " + labelFor(ptype, value), describe(ptype, value)) }];
    if (ptype === "color") {
      acts.push({
        label: "Mix with…  (accept a color)", run: async () => {
          const r = await accept("color", "MIX " + value + " — click another COLOR anywhere (Esc cancels)");
          if (!r) return;
          const m = mixHex(value, r.value);
          world.addColor(m, value + " ⊕ " + r.value);
          world.log("mixed", { a: value, b: r.value, result: m });
          world.print([{ ptype: "color", value }, { text: " ⊕ " }, { ptype: "color", value: r.value }, { text: " = " }, { ptype: "color", value: m }]);
        },
      });
      acts.push({ label: "Lighten (new swatch)", run: () => world.addColor(rgbToHex(hexToRgb(value).map((x) => x + 30)), "lighten " + value) });
      if (world.colors.includes(value)) acts.push({ label: "Remove swatch", run: () => world.removeColor(value) });
    }
    if (ptype === "number") {
      acts.push({
        label: "Multiply by…  (accept a number)", run: async () => {
          const r = await accept("number", "MULTIPLY " + value + " — click another NUMBER anywhere (Esc cancels)");
          if (!r) return;
          world.print([{ text: value + " × " + r.value + " = " }, { ptype: "number", value: value * r.value }]);
          world.log("product", { a: value, b: r.value, result: value * r.value });
        },
      });
    }
    if (ptype === "note") acts.push({ label: "Remove note", run: () => world.removeNote(value) });
    if (ptype === "tile") {
      acts.push({ label: "Split ⬌ (new tile right)", run: () => splitLeaf(value, "row") });
      acts.push({ label: "Split ⬍ (new tile below)", run: () => splitLeaf(value, "col") });
      acts.push({
        label: "Swap app with…  (accept a tile)", run: async () => {
          const r = await accept("tile", "SWAP — click another TILE's title (Esc cancels)");
          if (r && r.value !== value) swapTiles(value, r.value);
        },
      });
      if (tree.type !== "leaf") acts.push({ label: "Close tile", run: () => closeLeaf(value) });
    }
    if (ptype === "workspace") {
      acts.push({ label: "Switch to", run: () => setCur(value) });
      acts.push({ label: "Rename", run: () => setRenaming(value) });
      acts.push({ label: "Duplicate", run: () => cloneSpace(value) });
      if (spaces.length > 1) acts.push({ label: "Delete", run: () => removeSpace(value) });
    }
    if (!["tile", "workspace", "note"].includes(ptype)) {
      acts.push({ label: "Collect into Notes", run: () => world.collect(ptype, value) });
    }
    return acts;
  };

  const ui = {
    world, accepting, setAccepting, setMouseDoc, accept, labelFor, describe, drag,
    openMenu: (ptype, value, x, y) => setMenu({ ptype, value, x, y }),
    wm: { setRatio, splitLeaf, closeLeaf, setLeafApp, startDrag, registerRef, canClose: tree.type !== "leaf" },
  };

  const dragSrcLeaf = drag && findLeaf(tree, drag.from);

  return (
    <UICtx.Provider value={ui}>
      <div onClick={() => setMenu(null)} style={{
        fontFamily: "'IBM Plex Mono', ui-monospace, Menlo, monospace", background: C.paper, color: C.ink,
        height: "100vh", display: "flex", flexDirection: "column", fontSize: 12,
      }}>
        <style>{`
          .pres { cursor: pointer; }
          .pres:hover { outline: 1px dotted ${C.ink}; background: ${C.sel}; }
          .pres.acceptable { outline: 2px solid ${C.red}; background: ${C.sel}; animation: pulse 0.9s infinite; cursor: pointer; }
          @keyframes pulse { 50% { outline-color: ${C.mustard}; } }
          ::-webkit-scrollbar { width: 12px; height: 12px; }
          ::-webkit-scrollbar-thumb { background: ${C.faint}; border: 3px solid ${C.pane}; }
          ::-webkit-scrollbar-track { background: ${C.paneAlt}; }
          @media (prefers-reduced-motion: reduce) { .pres.acceptable { animation: none; } }
        `}</style>

        <div style={{ background: C.ink, color: C.paper, textAlign: "center", padding: "4px 0", fontWeight: 700, letterSpacing: "0.3em", fontSize: 13, flexShrink: 0 }}>
          P B U I &nbsp; S H E L L &nbsp;—&nbsp; P R E S E N T A T I O N S , &nbsp; T I L E S , &nbsp; W O R K S P A C E S
        </div>

        {accepting && (
          <div style={{ background: C.red, color: C.paper, padding: "3px 10px", fontWeight: 700, flexShrink: 0 }}>
            ACCEPTING &lt;{Array.isArray(accepting.ptype) ? accepting.ptype.join("|") : accepting.ptype}&gt; — {accepting.prompt} — works across tiles AND workspaces
          </div>
        )}

        {/* workspace strip */}
        <div style={{ display: "flex", gap: 6, alignItems: "center", padding: "5px 8px 0", flexShrink: 0 }}>
          <span style={{ fontSize: 10, fontWeight: 700, letterSpacing: "0.08em" }}>WORKSPACES</span>
          {spaces.map((s) =>
            renaming === s.id ? (
              <input key={s.id} autoFocus defaultValue={s.name}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    const name = e.target.value.trim() || s.name;
                    setSpaces((ss) => ss.map((x) => (x.id === s.id ? { ...x, name } : x)));
                    world.log("workspace_renamed", { name });
                    setRenaming(null);
                  }
                }}
                onBlur={() => setRenaming(null)}
                style={{ border: "2px solid " + C.ink, background: "#fbf8ef", fontFamily: "inherit", fontSize: 11, padding: "1px 5px", width: 90 }} />
            ) : (
              <P key={s.id} ptype="workspace" value={s.id} onActivate={() => setCur(s.id)} activateDoc="switch to it"
                doc={"workspace " + s.name + " (" + countLeaves(s.tree) + " tiles)"}>
                <span style={{ border: "2px solid " + C.ink, background: cur === s.id ? C.sel : C.paneAlt, padding: "1px 9px", fontWeight: cur === s.id ? 700 : 400, cursor: "pointer" }}>{s.name}</span>
              </P>
            ))}
          <Btn tone={C.mint} onClick={addSpace}>+ workspace</Btn>
          <span style={{ color: C.faint, fontSize: 10.5 }}>chip: L switches · R for rename / duplicate / delete</span>
        </div>

        {/* the tiled desktop */}
        <div style={{ flex: 1, display: "flex", padding: 8, minHeight: 0 }}>
          <NodeView node={tree} />
        </div>

        {/* status / mouse-doc line */}
        <div style={{ background: C.ink, color: C.paper, padding: "3px 10px", fontSize: 11, flexShrink: 0, display: "flex", gap: 16 }}>
          <span style={{ color: C.mustard, fontWeight: 700 }}>{accepting ? "ACCEPT MODE" : drag ? "MOVING APP" : "READY"}</span>
          <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {mouseDoc || (accepting ? accepting.prompt + "   (Esc: abort)" : "click anything for its object menu (L = R, unless L already accepts or activates) · drag borders to resize (sticky at ¼ ⅓ ½ ⅔ ¾) · drag ⠿: center = swap, edge = split-dock (source tile closes) · tiles + workspaces are presentations too")}
          </span>
          <span style={{ color: C.faint }}>{countLeaves(tree)} tiles · {spaces.length} workspaces</span>
        </div>

        {/* drag ghost */}
        {drag && dragSrcLeaf && (
          <div style={{ position: "fixed", left: drag.x + 12, top: drag.y + 12, zIndex: 200, pointerEvents: "none", background: APPS[dragSrcLeaf.app].color, border: "2px solid " + C.ink, boxShadow: "3px 3px 0 " + C.ink, padding: "1px 8px", fontSize: 11, fontWeight: 700 }}>
            {APPS[dragSrcLeaf.app].title} → {drag.over && drag.over !== drag.from
              ? (drag.zone === "center" ? "swap apps"
                : "dock " + ({ left: "⇤", right: "⇥", top: "⤒", bottom: "⤓" }[drag.zone] || "") + " (source tile closes)")
              : "drop on a tile · center swaps · edges split"}
          </div>
        )}

        {/* object menu */}
        {menu && (
          <div onClick={(e) => e.stopPropagation()} style={{
            position: "fixed", left: Math.min(menu.x, (typeof window !== "undefined" ? window.innerWidth : 800) - 320), top: menu.y, zIndex: 100,
            background: C.pane, border: "2px solid " + C.ink, boxShadow: "4px 4px 0 " + C.ink, minWidth: 260,
          }}>
            <div style={{ background: C.ink, color: C.paper, padding: "2px 8px", fontSize: 10, fontWeight: 700, letterSpacing: "0.06em" }}>
              &lt;{menu.ptype}&gt; {labelFor(menu.ptype, menu.value).slice(0, 26)}
            </div>
            {actionsFor(menu.ptype, menu.value).map((a, i) => (
              <div key={i} onClick={() => { setMenu(null); a.run(); }}
                style={{ padding: "4px 10px", cursor: "pointer", borderTop: i ? "1px dotted " + C.faint : "none", fontSize: 11.5 }}
                onMouseEnter={(e) => (e.currentTarget.style.background = C.sel)} onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}>
                {a.label}
              </div>
            ))}
          </div>
        )}
      </div>
    </UICtx.Provider>
  );
}
