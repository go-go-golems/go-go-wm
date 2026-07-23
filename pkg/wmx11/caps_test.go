package wmx11

import (
	"strings"
	"testing"
)

func TestCapabilityMintCheckRevoke(t *testing.T) {
	w := newTestWM()
	c := w.mintCapability("capsule/1", ActionWindowRead, "wm.window/0x100")
	if !strings.HasPrefix(c.ID, "cap:") || len(c.ID) < 20 {
		t.Fatalf("weak capability id %q", c.ID)
	}

	if err := w.checkCapability(c.ID, ActionWindowRead, "wm.window/0x100"); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
	// Wrong ref, wrong action, unknown id: all rejected.
	if err := w.checkCapability(c.ID, ActionWindowRead, "wm.window/0x200"); err == nil {
		t.Fatal("grant leaked to another ref")
	}
	if err := w.checkCapability(c.ID, "wm.window.write", "wm.window/0x100"); err == nil {
		t.Fatal("grant leaked to another action")
	}
	if err := w.checkCapability("cap:bogus", ActionWindowRead, "wm.window/0x100"); err == nil {
		t.Fatal("unknown capability accepted")
	}

	// Revocation by holder removes the grant; revoking again is a no-op.
	other := w.mintCapability("capsule/2", ActionWindowRead, "wm.window/0x300")
	w.revokeCapabilitiesFor("capsule/1")
	w.revokeCapabilitiesFor("capsule/1")
	if err := w.checkCapability(c.ID, ActionWindowRead, "wm.window/0x100"); err == nil {
		t.Fatal("revoked capability still valid")
	}
	if err := w.checkCapability(other.ID, ActionWindowRead, "wm.window/0x300"); err != nil {
		t.Fatalf("unrelated holder's grant was revoked: %v", err)
	}
}
