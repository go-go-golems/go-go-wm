# Third Codex review batch (commit 6635c69, captured 2026-07-20)

## RC-11 — pkg/xgojaprovider/provider.go:108: When a generated runtime enables both `pbui` and `wm`, each factory creates an independent `runtimeState`, so requiring both modules opens two broker connections with the same default client name. The broker routes `verb.run` by owner name to the first matching connection, making a verb registered through `pbui` nondeterministically arrive on the `wm` connection, where it has no verb handler and is silently lost; disconnecting either connection also removes all verbs under that shared name. Both factories need to reuse one runtime-scoped client and event fan.

**<sub><sub>![P1 Badge](https://img.shields.io/badge/P1-orange?style=flat)</sub></sub>  Share one broker client across provider modules**

When a generated runtime enables both `pbui` and `wm`, each factory creates an independent `runtimeState`, so requiring both modules opens two broker connections with the same default client name. The broker routes `verb.run` by owner name to the first matching connection, making a verb registered through `pbui` nondeterministically arrive on the `wm` connection, where it has no verb handler and is silently lost; disconnecting either connection also removes all verbs under that shared name. Both factories need to reuse one runtime-scoped client and event fan.

Useful? React with 👍 / 👎.

## RC-12 — pkg/wmx11/manage.go:589: When a floating client is fullscreen and sends a `ConfigureRequest`—for example, a dialog resizing itself after its contents change—this path still applies the requested geometry through `configureFloat`. The frame then stops covering the screen while `w.fullscreen` remains set, and subsequent relayouts deliberately skip it, leaving the WM in a visibly non-fullscreen but fullscreen-locked state until the user toggles the mode off and on.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Ignore float configure requests while fullscreen**

When a floating client is fullscreen and sends a `ConfigureRequest`—for example, a dialog resizing itself after its contents change—this path still applies the requested geometry through `configureFloat`. The frame then stops covering the screen while `w.fullscreen` remains set, and subsequent relayouts deliberately skip it, leaving the WM in a visibly non-fullscreen but fullscreen-locked state until the user toggles the mode off and on.

Useful? React with 👍 / 👎.

## RC-13 — pkg/wmx11/manage.go:521: When a naturally floating dialog is fullscreen and a navigation binding such as Mod4-space calls `focus`, clearing `w.focused` here discards the tiled leaf that `focusFloat` intentionally preserves for focus restoration. After fullscreen is exited and the dialog closes, `unmanageFloat` cannot focus the previous tile because the register is empty, leaving no managed window focused until another explicit navigation or click.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Preserve tiled focus beneath a fullscreen float**

When a naturally floating dialog is fullscreen and a navigation binding such as Mod4-space calls `focus`, clearing `w.focused` here discards the tiled leaf that `focusFloat` intentionally preserves for focus restoration. After fullscreen is exited and the dialog closes, `unmanageFloat` cannot focus the previous tile because the register is empty, leaving no managed window focused until another explicit navigation or click.

Useful? React with 👍 / 👎.

## RC-14 — pkg/cmds/run.go:236: For `run`/`repl --ui` scripts with a live `ui.app().show()` surface, this handler runs on the event-fan drainer while the X app loop may be inside `uispec.Render`. `draw.SetTheme` rewrites the package-level palette and `AppColors` without synchronization, despite those values being read by renderers on the X loop, so a theme event can cause a data race and a frame assembled from mixed old/new colors. The palette change must be coordinated with every surface's render loop or made atomic/immutable.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Serialize palette swaps with surface rendering**

For `run`/`repl --ui` scripts with a live `ui.app().show()` surface, this handler runs on the event-fan drainer while the X app loop may be inside `uispec.Render`. `draw.SetTheme` rewrites the package-level palette and `AppColors` without synchronization, despite those values being read by renderers on the X loop, so a theme event can cause a data race and a frame assembled from mixed old/new colors. The palette change must be coordinated with every surface's render loop or made atomic/immutable.

Useful? React with 👍 / 👎.

## RC-15 — pkg/xshm/xshm.go:103: On an X server whose root depth is not the assumed 24-bit/32-bpp format, `Available` can still return true and this creates a pixmap at that depth while allocating and writing an unconditional four-byte BGRA pixel layout. A 16-bit or other nonmatching visual therefore interprets the shared memory with the wrong pixel format, producing corrupted frames instead of falling back to the existing PutImage path; validate the visual format or derive its bytes-per-pixel before creating the surface.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Reject incompatible root depths before enabling SHM**

On an X server whose root depth is not the assumed 24-bit/32-bpp format, `Available` can still return true and this creates a pixmap at that depth while allocating and writing an unconditional four-byte BGRA pixel layout. A 16-bit or other nonmatching visual therefore interprets the shared memory with the wrong pixel format, producing corrupted frames instead of falling back to the existing PutImage path; validate the visual format or derive its bytes-per-pixel before creating the surface.

Useful? React with 👍 / 👎.

## RC-16 — pkg/launcher/frecency.go:92: If the debounce timer has already entered `save`, it snapshots the entries and unlocks during disk I/O; a launch can then add a newer entry and shutdown can call `Flush`. This `saving` branch makes that flush return immediately after cancelling the newly scheduled timer, so the in-flight write persists only its older snapshot and the final launch is lost. `Flush` needs to wait for the active save and then persist any updates made after its snapshot.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Make shutdown flush wait for an in-flight save**

If the debounce timer has already entered `save`, it snapshots the entries and unlocks during disk I/O; a launch can then add a newer entry and shutdown can call `Flush`. This `saving` branch makes that flush return immediately after cancelling the newly scheduled timer, so the in-flight write persists only its older snapshot and the final launch is lost. `Flush` needs to wait for the active save and then persist any updates made after its snapshot.

Useful? React with 👍 / 👎.
