package wmx11

// Script tiles (GGWM-003 U3): leaves whose App is "script:<name>" render
// through a registry of Go closures the in-process ui module installs.
// The closures are VM-free — they read spec snapshots — so the WM loop
// paints script tiles exactly like builtins, and never touches goja
// (design decision U-D3).

import (
	"fmt"
	"image"
	"strings"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
)

const scriptPrefix = "script:"

// scriptTile is one registered scripted surface.
type scriptTile struct {
	render func(w, h int, accepting []string) (*image.RGBA, []apps.Region)
	action func(action string)
	key    func(key string)
}

// RegisterTile installs a script tile under "script:<name>" (WM loop
// via post; duplicate names error). Part of the uimod.TileHost seam.
func (b *ScriptBackend) RegisterTile(name string,
	render func(w, h int, accepting []string) (*image.RGBA, []apps.Region),
	action func(action string),
	key func(key string)) error {
	if name == "" || render == nil {
		return fmt.Errorf("script tile needs a name and a renderer")
	}
	errCh := make(chan error, 1)
	b.WM.Post(func() {
		if b.WM.scriptTiles == nil {
			b.WM.scriptTiles = map[string]*scriptTile{}
		}
		if _, dup := b.WM.scriptTiles[name]; dup {
			errCh <- fmt.Errorf("script tile %q already registered", name)
			return
		}
		b.WM.scriptTiles[name] = &scriptTile{render: render, action: action, key: key}
		// A leaf may already point at this tile (rc.js re-run, layout
		// recipe): repaint everything that references it.
		b.WM.repaintScriptTile(name)
		errCh <- nil
	})
	select {
	case err := <-errCh:
		return err
	case <-b.WM.ctx.Done():
		return fmt.Errorf("wm shutting down")
	}
}

// RepaintTile schedules a repaint of the tile's frames (any goroutine).
func (b *ScriptBackend) RepaintTile(name string) {
	b.WM.Post(func() { b.WM.repaintScriptTile(name) })
}

// repaintScriptTile repaints every frame showing "script:<name>". WM loop.
func (w *WM) repaintScriptTile(name string) {
	app := scriptPrefix + name
	for i := range w.desktop.Workspaces {
		for _, l := range w.desktop.Workspaces[i].Root.Leaves() {
			if l.App != app {
				continue
			}
			if f, ok := w.frames[l.ID]; ok && f.client == 0 {
				w.paintFrame(f)
			}
		}
	}
}

// renderScriptTile paints a registered tile, or a placeholder naming the
// missing registration. WM loop.
func (w *WM) renderScriptTile(name string, cw, ch int, accepting []string) (*image.RGBA, []apps.Region) {
	tile := w.scriptTiles[strings.TrimPrefix(name, scriptPrefix)]
	if tile == nil {
		img := apps.NewSurface(cw, ch)
		draw.Text(img, 8, 20, "script tile "+name, true, 11, draw.Ink)
		draw.Text(img, 8, 36, "not registered — load its rc.js app and call app.tile()", false, 10.5, draw.Faint)
		return img, nil
	}
	return tile.render(cw, ch, accepting)
}

// scriptTileAction routes a button action to the tile's JS app (the
// closure posts to the JS loop; nothing runs here).
func (w *WM) scriptTileAction(name, action string) bool {
	tile := w.scriptTiles[strings.TrimPrefix(name, scriptPrefix)]
	if tile == nil || tile.action == nil {
		return false
	}
	tile.action(strings.TrimPrefix(action, "cmd:"))
	return true
}
