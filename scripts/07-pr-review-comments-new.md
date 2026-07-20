# New Codex review comments on commit ad7570b3 (captured 2026-07-20)

## RC-6 (P2) — pkg/wmx11/wm.go:387: When `wm.apply([...])` switches workspaces while a window is fullscreen, `ApplyBatch` calls `relayout` before reaching this branch, so the old fullscreen frame is unmapped while `w.fullscreen` remains set. `refocusCurrent` then calls `focus`, which pins focus back to that old fullscreen leaf, leaving the newly selected workspace without usable keyboard focus; mirror `afterOp` by exiting fullscreen before the batch reconciliation.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Exit fullscreen before reconciling a batch switch**

When `wm.apply([...])` switches workspaces while a window is fullscreen, `ApplyBatch` calls `relayout` before reaching this branch, so the old fullscreen frame is unmapped while `w.fullscreen` remains set. `refocusCurrent` then calls `focus`, which pins focus back to that old fullscreen leaf, leaving the newly selected workspace without usable keyboard focus; mirror `afterOp` by exiting fullscreen before the batch reconciliation.

Useful? React with 👍 / 👎.

## RC-6 (P2) — pkg/wmx11/manage.go:513: When the fullscreen frame is floating, its `leaf` is empty, so a navigation action such as the default Mod4-space replaces the target with `""` here and then clears `focusedFloat` below. X input may remain on the visible client, but the WM no longer considers any window focused, so after leaving fullscreen operations such as close, float, or fullscreen no longer target that window until the user clicks another frame; floating fullscreen frames need to remain tracked through `focusedFloat` rather than this leaf-only path.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Preserve focus state for fullscreen floating windows**

When the fullscreen frame is floating, its `leaf` is empty, so a navigation action such as the default Mod4-space replaces the target with `""` here and then clears `focusedFloat` below. X input may remain on the visible client, but the WM no longer considers any window focused, so after leaving fullscreen operations such as close, float, or fullscreen no longer target that window until the user clicks another frame; floating fullscreen frames need to remain tracked through `focusedFloat` rather than this leaf-only path.

Useful? React with 👍 / 👎.

## RC-6 (P2) — pkg/launcher/desktop.go:65: This cache only compares the application directory's mtime, but editing or overwriting the contents of an existing `.desktop` file does not change the directory mtime. After such an application update, `Refresh` therefore returns the cached command indefinitely, so the launcher can keep using an obsolete name or `Exec` line until an entry is added/removed or the WM restarts; track entry metadata or otherwise invalidate existing files too.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Rescan modified desktop entries**

This cache only compares the application directory's mtime, but editing or overwriting the contents of an existing `.desktop` file does not change the directory mtime. After such an application update, `Refresh` therefore returns the cached command indefinitely, so the launcher can keep using an obsolete name or `Exec` line until an entry is added/removed or the WM restarts; track entry metadata or otherwise invalidate existing files too.

Useful? React with 👍 / 👎.

## RC-6 (P2) — pkg/launcher/frecency.go:50: For every launch within five seconds of the preceding save, this returns without scheduling a later write, and the registry has no shutdown flush. If the WM exits before another launch after the interval, all usage recorded during that burst is lost, causing persisted frecency counts and ordering to lag; use an actual delayed flush or persist pending entries when the registry closes.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Flush debounced frecency updates**

For every launch within five seconds of the preceding save, this returns without scheduling a later write, and the registry has no shutdown flush. If the WM exits before another launch after the interval, all usage recorded during that burst is lost, causing persisted frecency counts and ordering to lag; use an actual delayed flush or persist pending entries when the registry closes.

Useful? React with 👍 / 👎.

## RC-6 (P2) — pkg/jsmod/uimod/app.go:199: A `ui.app` can be exposed through both documented surfaces by calling `show()` and `tile()`, but each surface calls `setRedraw` and this assignment replaces the previous callback. Consequently, actions, `refresh()`, and theme changes repaint only whichever surface registered last while the other continues displaying the old snapshot; store and invoke a redraw hook per live surface instead of a single slot.

**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Retain redraw hooks for every app surface**

A `ui.app` can be exposed through both documented surfaces by calling `show()` and `tile()`, but each surface calls `setRedraw` and this assignment replaces the previous callback. Consequently, actions, `refresh()`, and theme changes repaint only whichever surface registered last while the other continues displaying the old snapshot; store and invoke a redraw hook per live surface instead of a single slot.

Useful? React with 👍 / 👎.
