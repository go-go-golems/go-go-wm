package jsmod

import (
	"context"
	"time"

	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// EmitScriptError reports a failure inside script-supplied code as a
// script.error event on the broker bus. Scripts never kill the host: a
// throwing verb handler or an overflowing event queue becomes a trace
// line, visible in the WM's trace tile like every other event.
//
// Safe to call from any goroutine; fire-and-forget with a short deadline.
func EmitScriptError(cl *client.Client, where string, err error, extra map[string]interface{}) {
	if cl == nil {
		return
	}
	data := map[string]interface{}{"where": where}
	if err != nil {
		data["error"] = err.Error()
	}
	for k, v := range extra {
		data[k] = v
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = cl.Emit(ctx, "script.error", data)
	}()
}
