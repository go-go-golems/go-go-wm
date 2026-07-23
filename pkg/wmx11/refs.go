package wmx11

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jezek/xgb/xproto"
)

// Live window references (GGWM-013 M3).
//
// A ref names a managed client window: "wm.window/0x<xid>". Unlike a leaf id
// or a title it survives relayouts and renames, and unlike a bare xid it is
// typed — describe answers with a snapshot, and a destroyed window answers
// with a tombstone (last-known state plus when it died) instead of vanishing
// silently. Refs are knowledge, not permission: holding one grants nothing;
// the capability layer (M4) decides who may describe it.

const windowRefPrefix = "wm.window/"

// WindowDescription is the answer to describing a window ref: the current
// snapshot for a live window, or the last-known snapshot for a tombstone.
type WindowDescription struct {
	Ref   string `json:"ref"`
	Alive bool   `json:"alive"`
	WindowInfo
	DestroyedAt string `json:"destroyed_at,omitempty"`
}

// windowRef renders the canonical ref for a client window.
func windowRef(client xproto.Window) string {
	return fmt.Sprintf("%s0x%08x", windowRefPrefix, uint32(client))
}

// parseWindowRef accepts "wm.window/0x<hex>" or "wm.window/<decimal>".
func parseWindowRef(ref string) (xproto.Window, error) {
	rest, ok := strings.CutPrefix(ref, windowRefPrefix)
	if !ok || rest == "" {
		return 0, fmt.Errorf("not a window ref: %q", ref)
	}
	base := 10
	if h, ok := strings.CutPrefix(rest, "0x"); ok {
		rest, base = h, 16
	}
	n, err := strconv.ParseUint(rest, base, 32)
	if err != nil {
		return 0, fmt.Errorf("bad window ref %q: %w", ref, err)
	}
	return xproto.Window(n), nil
}

// tombstone preserves a destroyed window's last snapshot. Bounded: only the
// most recent maxTombstones destructions are kept.
type tombstone struct {
	info WindowInfo
	at   time.Time
}

const maxTombstones = 64

// recordTombstone captures a frame's last state before teardown. Called at
// the top of unmanage, which is the single entry point for both tiled and
// floating teardown. WM loop only.
func (w *WM) recordTombstone(f *frame) {
	if f == nil || f.client == 0 {
		return // builtin tiles have no client window and no ref
	}
	if w.tombstones == nil {
		w.tombstones = map[xproto.Window]tombstone{}
	}
	if len(w.tombstones) >= maxTombstones {
		var oldest xproto.Window
		oldestAt := time.Now()
		for xid, t := range w.tombstones {
			if t.at.Before(oldestAt) {
				oldest, oldestAt = xid, t.at
			}
		}
		delete(w.tombstones, oldest)
	}
	w.tombstones[f.client] = tombstone{info: w.infoFor(f), at: time.Now()}
}

// infoFor builds the WindowInfo row for one frame, tiled or floating.
// WM loop only.
func (w *WM) infoFor(f *frame) WindowInfo {
	info := WindowInfo{
		Leaf:     string(f.leaf),
		Client:   uint32(f.client),
		Title:    f.title,
		Class:    f.class,
		Instance: f.instance,
		Rect:     f.rect.String(),
	}
	if f.floating {
		info.Floating = true
		info.Leader = uint32(f.leader)
		info.Workspace = f.ws
		info.Focused = w.fstate.FocusedFloat() == f.client
	} else {
		info.Focused = w.fstate.Focused(f)
		if ws := w.desktop.FindLeafWorkspace(f.leaf); ws != nil {
			info.Workspace = ws.ID
		}
	}
	return info
}

// describeWindow resolves a ref to its current or last-known state.
// WM loop only.
func (w *WM) describeWindow(ref string) (WindowDescription, error) {
	xid, err := parseWindowRef(ref)
	if err != nil {
		return WindowDescription{}, err
	}
	if f := w.byClient[xid]; f != nil {
		return WindowDescription{Ref: windowRef(xid), Alive: true, WindowInfo: w.infoFor(f)}, nil
	}
	if t, ok := w.tombstones[xid]; ok {
		return WindowDescription{
			Ref: windowRef(xid), Alive: false, WindowInfo: t.info,
			DestroyedAt: t.at.Format(time.RFC3339),
		}, nil
	}
	return WindowDescription{}, fmt.Errorf("unknown window %s", ref)
}
