package wmx11

import (
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/xwindow"
)

// setupEWMH advertises what we support and plants the supporting-WM-check
// window so pagers and bars believe us.
func (w *WM) setupEWMH() {
	check, err := xwindow.Generate(w.X)
	if err == nil {
		_ = check.CreateChecked(w.X.RootWin(), -1, -1, 1, 1, 0)
		_ = ewmh.SupportingWmCheckSet(w.X, w.X.RootWin(), check.Id)
		_ = ewmh.SupportingWmCheckSet(w.X, check.Id, check.Id)
		_ = ewmh.WmNameSet(w.X, check.Id, "go-go-wm")
	}
	_ = ewmh.SupportedSet(w.X, []string{
		"_NET_SUPPORTED", "_NET_SUPPORTING_WM_CHECK", "_NET_WM_NAME",
		"_NET_CLIENT_LIST", "_NET_ACTIVE_WINDOW",
		"_NET_NUMBER_OF_DESKTOPS", "_NET_CURRENT_DESKTOP",
		"_NET_DESKTOP_NAMES", "_NET_WM_DESKTOP",
	})
	w.updateEWMH()
}

// updateEWMH republishes desktop and client properties after any change.
func (w *WM) updateEWMH() {
	names := make([]string, 0, len(w.desktop.Workspaces))
	cur := 0
	for i, ws := range w.desktop.Workspaces {
		names = append(names, ws.Name)
		if ws.ID == w.desktop.Current {
			cur = i
		}
	}
	_ = ewmh.NumberOfDesktopsSet(w.X, uint(len(names)))
	_ = ewmh.DesktopNamesSet(w.X, names)
	_ = ewmh.CurrentDesktopSet(w.X, uint(cur))

	clients := make([]xproto.Window, 0, len(w.byClient))
	for cw, f := range w.byClient {
		clients = append(clients, cw)
		for i, ws := range w.desktop.Workspaces {
			if ws.Root.FindLeaf(f.leaf) != nil {
				_ = ewmh.WmDesktopSet(w.X, cw, uint(i))
				break
			}
		}
	}
	_ = ewmh.ClientListSet(w.X, clients)
}
