package wmx11

import (
	"context"
	"fmt"

	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xevent"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// ScriptBackend adapts a running WM into the wmmod Backend shape for the
// in-process (rc.js) runtime — the A1 attachment point. Every method
// posts a closure onto the WM loop and waits, exactly like dispatchIPC,
// minus the socket. It never executes JavaScript (concurrency rule 2);
// Bind's fire callback is expected to be a single post back into the JS
// loop.
type ScriptBackend struct{ WM *WM }

// onLoop runs fn on the WM loop and waits for it, honoring ctx.
func (b *ScriptBackend) onLoop(ctx context.Context, fn func()) error {
	done := make(chan struct{})
	b.WM.Post(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-b.WM.ctx.Done():
		return fmt.Errorf("wm shutting down")
	}
}

func (b *ScriptBackend) Tree(ctx context.Context) (*wmcore.Desktop, error) {
	var raw []byte
	var err error
	if lerr := b.onLoop(ctx, func() { raw, err = b.WM.desktop.Serialize() }); lerr != nil {
		return nil, lerr
	}
	if err != nil {
		return nil, err
	}
	return wmcore.DeserializeDesktop(raw)
}

func (b *ScriptBackend) Windows(ctx context.Context) ([]WindowInfo, error) {
	var out []WindowInfo
	if lerr := b.onLoop(ctx, func() { out = b.WM.windowsSnapshot() }); lerr != nil {
		return nil, lerr
	}
	return out, nil
}

func (b *ScriptBackend) Apply(ctx context.Context, op wmcore.Op) (wmcore.Result, error) {
	var res wmcore.Result
	var err error
	if lerr := b.onLoop(ctx, func() { res, err = b.WM.Apply(op) }); lerr != nil {
		return res, lerr
	}
	return res, err
}

// Bind grabs combo on the root window; fire runs on the X event goroutine
// and must only post.
func (b *ScriptBackend) Bind(combo string, fire func()) error {
	errCh := make(chan error, 1)
	b.WM.Post(func() {
		errCh <- keybind.KeyPressFun(func(_ *xgbutil.XUtil, _ xevent.KeyPressEvent) {
			fire()
		}).Connect(b.WM.X, b.WM.X.RootWin(), combo, true)
	})
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("bind %s: %w", combo, err)
		}
		return nil
	case <-b.WM.ctx.Done():
		return fmt.Errorf("wm shutting down")
	}
}

func (b *ScriptBackend) Theme(ctx context.Context) (ThemeInfo, error) {
	var info ThemeInfo
	if lerr := b.onLoop(ctx, func() {
		info = ThemeInfo{Theme: draw.CurrentTheme(), Available: draw.ThemeNames()}
	}); lerr != nil {
		return info, lerr
	}
	return info, nil
}

func (b *ScriptBackend) SetTheme(ctx context.Context, name string) error {
	var err error
	if lerr := b.onLoop(ctx, func() { err = b.WM.setTheme(name) }); lerr != nil {
		return lerr
	}
	return err
}

func (b *ScriptBackend) Focus(ctx context.Context, target string) (string, error) {
	var err error
	var focused string
	if lerr := b.onLoop(ctx, func() {
		err = b.WM.focusTarget(target)
		focused = string(b.WM.focused)
	}); lerr != nil {
		return "", lerr
	}
	return focused, err
}

func (b *ScriptBackend) Move(ctx context.Context, dir string) (string, error) {
	var err error
	var focused string
	if lerr := b.onLoop(ctx, func() {
		err = b.WM.moveDir(dir)
		focused = string(b.WM.focused)
	}); lerr != nil {
		return "", lerr
	}
	return focused, err
}

// windowsSnapshot builds the WindowInfo table (WM loop only) — shared by
// the IPC dispatcher and ScriptBackend.
func (w *WM) windowsSnapshot() []WindowInfo {
	var out []WindowInfo
	for leaf, f := range w.frames {
		info := WindowInfo{
			Leaf:     string(leaf),
			Client:   uint32(f.client),
			Title:    f.title,
			Class:    f.class,
			Instance: f.instance,
			Rect:     f.rect.String(),
			Focused:  w.focused == leaf,
		}
		if ws := w.desktop.FindLeafWorkspace(leaf); ws != nil {
			info.Workspace = ws.ID
		}
		out = append(out, info)
	}
	return out
}
