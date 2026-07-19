package wmx11

import (
	"image/color"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xprop"
)

// pixel converts an RGBA color to a 24-bit X pixel value (TrueColor).
func pixel(c color.RGBA) uint32 {
	return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

func xprop_Atm(X *xgbutil.XUtil, name string) (xproto.Atom, error) {
	return xprop.Atm(X, name)
}

// clientMessage builds a 32-bit-format ClientMessageEvent.
func clientMessage(win xproto.Window, typ xproto.Atom, data0 uint32) (xproto.ClientMessageEvent, error) {
	return xproto.ClientMessageEvent{
		Format: 32,
		Window: win,
		Type:   typ,
		Data:   xproto.ClientMessageDataUnionData32New([]uint32{data0, 0, 0, 0, 0}),
	}, nil
}
