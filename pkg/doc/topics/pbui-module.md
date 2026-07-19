---
Title: The pbui scripting module
Slug: pbui-module
Topics:
- scripting
- pbui
IsTemplate: false
IsTopLevel: true
ShowPerDefault: true
SectionType: GeneralTopic
---

The `pbui` native module makes a script a first-class PBUI participant:
it can present typed objects, accept them from anywhere on the desktop,
and teach every other client new verbs. The script's filename is its
client name on the broker — and verb ownership.

## Objects (data-only, no broker needed)

- `pbui.object(ptype, value)` — build `{ptype, value}`. Ptypes are slugs
  (`[A-Za-z0-9._-]+`): "color", "file", "git-commit", …
- `pbui.uri(obj)` / `pbui.parse(uri)` — the `pbui://` URI bijection
  (what OSC 8 hyperlinks carry).
- `pbui.link(obj, text)` — text wrapped in an OSC 8 hyperlink for
  terminal output.

## The accept protocol

    const picked = await pbui.accept("color", "PICK — click any color");
    if (picked === null) { /* cancelled — a normal outcome */ }

`pbui.accept(ptypes, prompt?)` returns a Promise. The whole desktop
enters accepting mode; whatever matching presentation is clicked —
in any process — resolves it. Cancellation resolves `null`, never a
rejection.

To be the *answering* side: `pbui.onAcceptMode(fn)` /
`pbui.onAcceptClear(fn)` observe the desktop-wide session, and
`pbui.answer(session, obj)` / `pbui.cancel(session?)` resolve it.

## Verbs

    pbui.verb(
      { id: "git.checkout", label: "Checkout", ptypes: ["git-commit"],
        accepts: [] },
      async (commit) => { … });

The descriptor is validated at definition time. The handler runs in this
process whenever any client invokes the verb on a matching object
(right-click menus everywhere gain the entry). Re-registering an id
replaces the handler. `accepts` marks accept-composing verbs.

## Output and events

- `pbui.print(...segments)` — print to the desktop listener; string
  segments are text, object segments render as live presentations.
- `pbui.emit(event, data)` — raw event-bus emission.
- `pbui.on(event, fn)` — subscribe (`"*"` matches all). Delivery is
  bounded: a slow script drops (oldest kept) and a `script.error` event
  carries the drop count.
- `pbui.hover(text)` — set the mouse-doc line.
- `pbui.menu(obj, x?, y?)` — Promise of the verbs applicable to obj.

## Errors

A throwing handler never kills the process: it becomes a `script.error`
event on the bus, visible in the WM's trace tile.

## See also

`glaze help wm-module` for layout scripting.
