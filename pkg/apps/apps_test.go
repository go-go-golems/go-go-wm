package apps

import (
	"bytes"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

var update = flag.Bool("update", false, "regenerate golden PNGs")

func checkGolden(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	path := filepath.Join("testdata", name+".png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (go test ./pkg/apps -update): %v", path, err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		_ = os.WriteFile(path+".got.png", buf.Bytes(), 0o644)
		t.Errorf("%s differs from golden", name)
	}
}

func testWorld() *World {
	w := NewWorld()
	color, _ := pbui.NewObject("color", "#b0563f")
	num, _ := pbui.NewObject("number", 42)
	w.Print(Seg{Text: "picked "}, Seg{Object: &color})
	w.Print(Seg{Text: "6 x 7 = "}, Seg{Object: &num})
	w.AddTrace(TraceEvent{Seq: 1, Type: "accept.started", Data: "ptypes=[color]", Source: "demo-colors"})
	w.AddTrace(TraceEvent{Seq: 2, Type: "accept.answered", Data: "ptype=color by=wm", Source: "wm"})
	w.AddTrace(TraceEvent{Seq: 3, Type: "split-leaf", Data: "node=n1", Source: "wm"})
	return w
}

func TestGoldenBuiltins(t *testing.T) {
	world := testWorld()
	for _, name := range []string{AppLauncher, AppAbout, AppTrace, AppListener, AppInspector} {
		img, _ := RenderBuiltin(name, 560, 320, world, nil)
		checkGolden(t, "builtin-"+name, img)
	}
	// Accept highlight variant.
	img, _ := RenderBuiltin(AppListener, 560, 320, world, []string{"number"})
	checkGolden(t, "builtin-listener-accepting-number", img)
}

func TestGoldenDemos(t *testing.T) {
	colors := []string{"#b0563f", "#d3b56a", "#9cb4c2"}
	img, _ := RenderColors(560, 320, colors, nil)
	checkGolden(t, "demo-colors", img)
	img, _ = RenderNumbers(560, 320, []string{"number"})
	checkGolden(t, "demo-numbers-accepting", img)
	c, _ := pbui.NewObject("color", "#9cb4c2")
	n, _ := pbui.NewObject("number", 42)
	img, _ = RenderNotes(560, 320, []Note{{ID: 1, Obj: c}, {ID: 2, Obj: n}}, nil)
	checkGolden(t, "demo-notes", img)
}

func TestRegionsAndResolve(t *testing.T) {
	world := testWorld()
	_, regions := RenderBuiltin(AppListener, 560, 320, world, nil)
	if len(regions) < 4 {
		t.Fatalf("listener should expose buttons + chips, got %d regions", len(regions))
	}
	// A button region resolves to its action.
	var btn *Region
	for i := range regions {
		if regions[i].Action == "cmd:sum" {
			btn = &regions[i]
		}
	}
	if btn == nil {
		t.Fatal("no cmd:sum region")
	}
	if c := Resolve(nil, btn, 1); c.Action != "cmd:sum" {
		t.Fatalf("Resolve(button) = %+v", c)
	}
	// A chip region answers a matching accept…
	var chip *Region
	for i := range regions {
		if regions[i].Object != nil && regions[i].Object.Ptype == "number" {
			chip = &regions[i]
		}
	}
	if chip == nil {
		t.Fatal("no number chip region")
	}
	if c := Resolve([]string{"number"}, chip, 1); c.Answer == nil {
		t.Fatalf("chip should answer accept: %+v", c)
	}
	// …menus without one…
	if c := Resolve(nil, chip, 1); c.Menu == nil {
		t.Fatalf("chip should menu when idle: %+v", c)
	}
	// …and always menus on right click.
	if c := Resolve([]string{"number"}, chip, 3); c.Menu == nil || c.Answer != nil {
		t.Fatalf("right click must menu: %+v", c)
	}
}
