package wmx11

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Capabilities (GGWM-013 M4).
//
// A capability is an unforgeable grant: this HOLDER may perform this ACTION
// on this RESOURCE. v0 has exactly one action — "wm.window.read" — and one
// mint site: invoking the Explain-window verb mints a capability for the
// capsule it spawns, scoped to that one window ref. What v0 establishes is
// the mint → check → revoke loop; policy, attenuation, and powerbox prompts
// extend this rather than replace it.
//
// The store is owned by the WM loop like all WM state. IDs carry 128 bits
// of randomness so holding an ID is holding the grant — nothing enumerates
// them back out except the trusted {"q":"sem"} debug dump.

// ActionWindowRead is the one v0 action: resolve a window ref via describe.
const ActionWindowRead = "wm.window.read"

type capability struct {
	ID     string `json:"id"`
	Holder string `json:"holder"` // capsule id, or a broker principal later
	Action string `json:"action"`
	Ref    string `json:"ref"` // the one resource this grant covers
}

// mintCapability creates and stores a grant. WM loop only.
func (w *WM) mintCapability(holder, action, ref string) *capability {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the system is broken in ways beyond a
		// capability ID; fall back to a counter so the WM stays up.
		w.nextCap++
		return w.storeCap(&capability{
			ID:     fmt.Sprintf("cap:seq/%d", w.nextCap),
			Holder: holder, Action: action, Ref: ref,
		})
	}
	return w.storeCap(&capability{
		ID:     "cap:" + hex.EncodeToString(b[:]),
		Holder: holder, Action: action, Ref: ref,
	})
}

func (w *WM) storeCap(c *capability) *capability {
	if w.caps == nil {
		w.caps = map[string]*capability{}
	}
	w.caps[c.ID] = c
	return c
}

// checkCapability verifies that capID grants action on ref. WM loop only.
func (w *WM) checkCapability(capID, action, ref string) error {
	c := w.caps[capID]
	if c == nil {
		return fmt.Errorf("capability revoked or unknown")
	}
	if c.Action != action || c.Ref != ref {
		return fmt.Errorf("capability does not cover %s on %s", action, ref)
	}
	return nil
}

// revokeCapabilitiesFor drops every grant a holder has — the capability
// half of ending a capsule's lease. Idempotent. WM loop only.
func (w *WM) revokeCapabilitiesFor(holder string) {
	for id, c := range w.caps {
		if c.Holder == holder {
			delete(w.caps, id)
		}
	}
}

// DescribeWith is the capability-gated describe used by capsule runtimes:
// same answer as Describe, reachable only with a live grant covering the
// ref (GGWM-013 M4).
func (b *ScriptBackend) DescribeWith(capID, ref string) (WindowDescription, error) {
	var desc WindowDescription
	var err error
	if lerr := b.onLoop(b.WM.ctx, func() {
		if err = b.WM.checkCapability(capID, ActionWindowRead, ref); err != nil {
			return
		}
		desc, err = b.WM.describeWindow(ref)
	}); lerr != nil {
		return desc, lerr
	}
	return desc, err
}
