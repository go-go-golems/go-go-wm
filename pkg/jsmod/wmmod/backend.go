// Package wmmod is the `wm` native module: layout queries and mutations
// exposed to goja scripts. Every mutation compiles to a serializable
// wmcore.Op — the same vocabulary the keyboard, mouse, and IPC speak —
// and goes through a Backend, which is the seam that lets one module
// serve two attachment points: over the control socket (standalone
// scripts, this file) and in-process (rc.js, backend_inproc.go).
package wmmod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
	"github.com/go-go-golems/go-go-wm/pkg/wmx11"
)

// Backend is what the module needs from a window manager.
type Backend interface {
	// Tree returns the full desktop (workspaces + current).
	Tree(ctx context.Context) (*wmcore.Desktop, error)
	// Windows returns the managed-window table.
	Windows(ctx context.Context) ([]wmx11.WindowInfo, error)
	// Apply executes one op.
	Apply(ctx context.Context, op wmcore.Op) (wmcore.Result, error)
	// ApplyBatch executes ops in order with one reconcile at the end.
	ApplyBatch(ctx context.Context, ops []wmcore.Op) ([]wmcore.Result, error)
	// Bind registers a keybinding (in-process runtimes only).
	Bind(combo string, fire func()) error
	// Theme returns the current theme name and the available names.
	Theme(ctx context.Context) (wmx11.ThemeInfo, error)
	// SetTheme swaps the WM's theme and repaints.
	SetTheme(ctx context.Context, name string) error
	// Focus focuses a leaf id or a direction (left|right|up|down|next|prev);
	// returns the focused leaf afterwards.
	Focus(ctx context.Context, target string) (string, error)
	// Move swaps the focused leaf with its neighbor in dir; returns the
	// focused leaf afterwards.
	Move(ctx context.Context, dir string) (string, error)
}

// ErrNoKeybindings is returned by backends that cannot grab keys.
var ErrNoKeybindings = errors.New(
	"keybindings require the in-process runtime; put this in rc.js (go-go-wm wm --rc)")

// IPCBackend implements Backend over the WM control socket — the A2
// attachment point. Stateless; safe from any goroutine.
type IPCBackend struct {
	Socket string // empty → wmx11.DefaultIPCSocketPath()
}

type ipcEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

// query runs one request/response against the socket, honoring ctx (the
// socket call itself is synchronous, so it runs in a goroutine).
func (b *IPCBackend) query(ctx context.Context, req interface{}, out interface{}) error {
	type result struct {
		env ipcEnvelope
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var r result
		r.err = wmx11.QueryIPC(b.Socket, req, &r.env)
		ch <- r
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if !r.env.OK {
			return fmt.Errorf("wm: %s", r.env.Error)
		}
		if out != nil {
			return json.Unmarshal(r.env.Data, out)
		}
		return nil
	}
}

func (b *IPCBackend) Tree(ctx context.Context) (*wmcore.Desktop, error) {
	var d wmcore.Desktop
	if err := b.query(ctx, map[string]string{"q": "tree"}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (b *IPCBackend) Windows(ctx context.Context) ([]wmx11.WindowInfo, error) {
	var wins []wmx11.WindowInfo
	if err := b.query(ctx, map[string]string{"q": "windows"}, &wins); err != nil {
		return nil, err
	}
	return wins, nil
}

func (b *IPCBackend) Apply(ctx context.Context, op wmcore.Op) (wmcore.Result, error) {
	var res wmcore.Result
	req := map[string]interface{}{"q": "op", "op": op}
	if err := b.query(ctx, req, &res); err != nil {
		return res, err
	}
	return res, nil
}

func (b *IPCBackend) ApplyBatch(ctx context.Context, ops []wmcore.Op) ([]wmcore.Result, error) {
	var res []wmcore.Result
	req := map[string]interface{}{"q": "batch", "ops": ops}
	if err := b.query(ctx, req, &res); err != nil {
		return res, err
	}
	return res, nil
}

func (b *IPCBackend) Bind(string, func()) error { return ErrNoKeybindings }

func (b *IPCBackend) Theme(ctx context.Context) (wmx11.ThemeInfo, error) {
	var info wmx11.ThemeInfo
	if err := b.query(ctx, map[string]string{"q": "theme"}, &info); err != nil {
		return info, err
	}
	return info, nil
}

func (b *IPCBackend) SetTheme(ctx context.Context, name string) error {
	return b.query(ctx, map[string]string{"q": "set-theme", "theme": name}, nil)
}

func (b *IPCBackend) Focus(ctx context.Context, target string) (string, error) {
	var focused string
	if err := b.query(ctx, map[string]string{"q": "focus", "target": target}, &focused); err != nil {
		return "", err
	}
	return focused, nil
}

func (b *IPCBackend) Move(ctx context.Context, dir string) (string, error) {
	var focused string
	if err := b.query(ctx, map[string]string{"q": "move", "dir": dir}, &focused); err != nil {
		return "", err
	}
	return focused, nil
}
