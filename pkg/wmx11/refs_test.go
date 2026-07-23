package wmx11

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jezek/xgb/xproto"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

func TestWindowRefRoundTrip(t *testing.T) {
	for _, xid := range []xproto.Window{1, 0x04a0000c, 0xffffffff} {
		ref := windowRef(xid)
		got, err := parseWindowRef(ref)
		if err != nil {
			t.Fatalf("parse(%q): %v", ref, err)
		}
		if got != xid {
			t.Fatalf("round trip %q: got 0x%x want 0x%x", ref, got, xid)
		}
	}
	// Decimal form is accepted too.
	if got, err := parseWindowRef("wm.window/42"); err != nil || got != 42 {
		t.Fatalf("decimal ref: got %v, %v", got, err)
	}
	for _, bad := range []string{"", "wm.window/", "wm.tile/x", "wm.window/0xzz"} {
		if _, err := parseWindowRef(bad); err == nil {
			t.Fatalf("parse(%q) should fail", bad)
		}
	}
}

// newRefTestWM extends the shared newTestWM with the maps the ref layer
// reads.
func newRefTestWM() *WM {
	w := newTestWM()
	w.desktop = wmcore.NewDesktop("")
	w.byClient = map[xproto.Window]*frame{}
	return w
}

func TestDescribeWindowLiveAndTombstone(t *testing.T) {
	w := newRefTestWM()
	f := &frame{client: 0x100, title: "editor", class: "XTerm", leaf: "l-1"}
	w.byClient[f.client] = f

	desc, err := w.describeWindow("wm.window/0x100")
	if err != nil {
		t.Fatal(err)
	}
	if !desc.Alive || desc.Title != "editor" || desc.Ref != windowRef(0x100) {
		t.Fatalf("live describe wrong: %+v", desc)
	}

	// Teardown records a tombstone; the ref keeps answering.
	w.recordTombstone(f)
	delete(w.byClient, f.client)

	desc, err = w.describeWindow("wm.window/0x100")
	if err != nil {
		t.Fatal(err)
	}
	if desc.Alive || desc.Title != "editor" || desc.DestroyedAt == "" {
		t.Fatalf("tombstone describe wrong: %+v", desc)
	}

	if _, err := w.describeWindow("wm.window/0x999"); err == nil || !strings.Contains(err.Error(), "unknown window") {
		t.Fatalf("unknown ref: %v", err)
	}
}

func TestTombstonesBounded(t *testing.T) {
	w := newRefTestWM()
	for i := 0; i < maxTombstones+10; i++ {
		w.recordTombstone(&frame{client: xproto.Window(0x1000 + i), title: fmt.Sprint(i), leaf: "l"})
	}
	if len(w.tombstones) != maxTombstones {
		t.Fatalf("tombstones = %d, want bound %d", len(w.tombstones), maxTombstones)
	}
	// Builtin tiles (client == 0) never tombstone.
	w.recordTombstone(&frame{client: 0, title: "builtin"})
	if _, ok := w.tombstones[0]; ok {
		t.Fatal("builtin tile got a tombstone")
	}
}
