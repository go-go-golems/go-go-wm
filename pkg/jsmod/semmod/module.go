// Package semmod is the `sem` native module for capsule runtimes
// (GGWM-013 M5): the ONLY window into the semantic kernel a capsule gets.
//
// Where rc.js receives the whole wm/pbui/ui surface, a capsule receives
// this module bound to exactly the grants it was spawned with. v0 carries
// one grant: describe() resolves the single window ref the capsule's
// capability covers. There is no ambient lookup — the module is
// constructed around closures the host built for this capsule, so the
// absence of a capability is enforced below the JavaScript API surface
// (research axiom 5).
package semmod

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
)

// ModuleName is what capsule scripts require().
const ModuleName = "sem"

// Module binds one capsule's grants. The host constructs the closures;
// they must be callable from the JS loop and post to their owning loops
// internally (Describe posts to the WM loop and waits).
type Module struct {
	// Ref is the window ref this capsule's capability covers.
	Ref string
	// Describe resolves Ref through the capability-gated path. The
	// returned value must be plain data (maps/slices/strings), ready for
	// vm.ToValue.
	Describe func() (interface{}, error)
}

// Loader installs the exports.
func (m *Module) Loader() require.ModuleLoader {
	return func(vm *goja.Runtime, module *goja.Object) {
		exports := module.Get("exports").(*goja.Object)
		set := func(name string, v interface{}) {
			if err := exports.Set(name, v); err != nil {
				panic(fmt.Errorf("semmod: set %s: %w", name, err))
			}
		}
		set("ref", func(goja.FunctionCall) goja.Value { return vm.ToValue(m.Ref) })
		set("describe", func(goja.FunctionCall) goja.Value {
			if m.Describe == nil {
				panic(vm.ToValue("sem.describe: no grant bound"))
			}
			v, err := m.Describe()
			if err != nil {
				panic(vm.ToValue("sem.describe: " + err.Error()))
			}
			return vm.ToValue(v)
		})
	}
}
