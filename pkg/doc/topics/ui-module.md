---
Title: The ui scripting module
Slug: ui-module
Topics:
- scripting
- ui
IsTemplate: false
IsTopLevel: true
ShowPerDefault: true
SectionType: GeneralTopic
---

The `ui` native module lets a script *be* an application: it describes a
surface as rows of segments, and Go renders it in the paper-and-ink look
with the full PBUI click contract — objects pulse during accepts, answer
clicks, and carry verb menus, exactly like the built-in apps.

## Builders (data-only)

    ui.row(...segments)                  one horizontal band (wraps)
    ui.text("s", {bold?, size?})         plain text
    ui.hint("s")                         faint annotation text
    ui.object(ptype, value, {label?, doc?})   a live presentation chip
    ui.button("label", "action", {color?, doc?})  color: rose|blue|mint|mustard|lavender|sage

## Defining an app

    const app = ui.app({
      name: "js-colors",                 // broker client name / verb owner
      title: "JS COLORS",
      render() { return [ui.row(...)]; },
      actions: { add() { ... } },        // button actions by name
      verbs: [{ id, label, ptypes, accepts?, run(obj) { ... } }],
      onKey(key) { ... },                // optional keyboard hook
    });

`render()` is re-run after every handler; specs are validated when
produced (bad kinds, non-slug ptypes, unknown keys throw immediately). A
throwing render leaves the previous frame on screen and emits a
`script.error` event.

## Showing it

- `app.show()` — a standalone X window (works from `go-go-wm run`; the
  process stays alive in daemon mode serving the app).
- `app.tile()` — register with the WM as a scripted tile
  (`go-go-wm wm --rc` runtimes only). Returns the app string
  `"script:<name>"`; place it like anything else:

      wm.split(wm.focused(), "row", { app: app.tile() });

- `app.refresh()` — re-render outside a handler (timers, `pbui.on`
  subscriptions).

## The concurrency shape (why it never deadlocks)

Handlers run on the script's JS loop; render surfaces (the X window, the
WM) only ever read the last normalized spec snapshot. Clicks post to the
JS loop; new snapshots post back. No render path calls JavaScript.

## xgoja

The `wm`, `pbui`, and `ui` modules are also packaged as the xgoja
provider `github.com/go-go-golems/go-go-wm/pkg/xgojaprovider` (package
id `go-go-wm`), so generated binaries can compile them in. Modules
connect lazily; config keys: `socket`, `wm_socket`, `display`, `name`.

## See also

`glaze help wm-module`, `glaze help pbui-module`.
