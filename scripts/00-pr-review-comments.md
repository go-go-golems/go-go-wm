# PR #1 Codex Review Comments (captured 2026-07-19)

Source: https://github.com/go-go-golems/go-go-wm/pull/1
Reviewer: chatgpt-codex-connector (Codex automated review)
Reviewed commit: 7e7639b2ca

## RC-1 (P1) — pkg/cmds/replui.go lines 93-95: Distinguish parse failures before retrying the cell

```go
    if err == nil && resp.Cell != nil && strings.Contains(resp.Cell.Execution.Error, "SyntaxError") {
        captured = false
        resp, err = k.app.Evaluate(ctx, k.sid, input)
```

A runtime SyntaxError is indistinguishable here from failure to parse the
expression wrapper, so inputs such as `(globalThis.n = (globalThis.n || 0) + 1, JSON.parse("{"))`
execute the increment, throw, and are then evaluated a second time. Any side effects
preceding a runtime-generated SyntaxError therefore happen twice; determine whether
the wrapper parses before executing it rather than inspecting the execution error text.

## RC-2 (P1) — pkg/cmds/replui.go lines 328-330: Serialize notebook evaluations through one worker

```go
    go func(n int, src string) {
        console, errText, result, val := a.kernel.eval(a.root, n, src)
        ctx.Post(func() {
```

When a user submits another cell before the previous one completes, each submission
starts an independent goroutine even though eval performs an Evaluate followed by a
separate WithRuntime capture. Those calls can interleave or begin out of submission
order; in particular, the global __pbui_console buffer can be drained by the wrong cell,
and later cells can observe history before earlier capture work completes. Queue the
complete evaluate-and-capture operation so notebook execution and output attribution
remain ordered.

## RC-3 (P1) — pkg/jsmod/eventfan.go lines 118-121: Copy Go subscriptions before releasing the mutex

```go
                f.mu.Lock()
                services, hasJS := f.services, f.hasServices && f.jsHandlerCount() > 0
                goSubs := f.goSubs
                f.mu.Unlock()
```

This copies only the map header, then iterates the shared map after unlocking. A dynamic
wm.rule or wm.command calls SubscribeGo while the event pump is already running, so an
arriving event can make the drainer read goSubs concurrently with a map write, producing
a data race or fatal concurrent map read and map write; copy the relevant handler slices
or the whole map while holding f.mu.

## RC-4 (P1) — pkg/repl/value.go lines 84-85: Use the evaluated value as the rich object's payload

```go
    if raw, err := json.Marshal(m); err == nil {
        v.Raw = raw
```

For a result implementing __pbui__, this stores the presentation descriptor itself in
Value.Raw, even though Raw becomes the pbui.Object.Value passed to accepts and verbs.
Consequently, clicking a custom matrix result sends {ptype, summary, views, ...} rather
than the evaluated matrix's exported data, so downstream consumers receive the wrong value;
preserve the captured exported value as the object payload and use the descriptor only for
display metadata.

## RC-5 (P2) — pkg/wmx11/manage.go lines 506-508: Exit fullscreen before focusing another tile

```go
func (w *WM) focus(leaf wmcore.NodeID) {
    prev := w.focused
    w.focused = leaf
```

While a frame is fullscreen, navigation such as the default Mod4-space or wm.focus("next")
reaches this method and transfers X input focus to another tiled client without removing
or changing the fullscreen frame stacked above it. The user continues seeing the original
fullscreen window while keystrokes go to the hidden client; either keep focus constrained
to the fullscreen frame or exit fullscreen before changing tiles.
