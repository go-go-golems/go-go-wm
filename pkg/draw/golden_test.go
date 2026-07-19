package draw

import (
	"bytes"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
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
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (run: go test ./pkg/draw -update): %v", path, err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		got := path + ".got.png"
		_ = os.WriteFile(got, buf.Bytes(), 0o644)
		t.Errorf("%s differs from golden (wrote %s)", name, got)
	}
}

func TestGoldenTitleStrip(t *testing.T) {
	checkGolden(t, "title-strip", TitleStrip{Title: "color lab", Color: Rose, Width: 420}.Render())
	checkGolden(t, "title-strip-focused", TitleStrip{Title: "listener", Color: Mint, Focused: true, Width: 420}.Render())
}

func TestGoldenBanner(t *testing.T) {
	checkGolden(t, "banner", Banner{
		Ptypes: []string{"color"},
		Prompt: "MIX #b0563f — click a COLOR anywhere",
		Width:  900,
	}.Render())
}

func TestGoldenStatusLine(t *testing.T) {
	checkGolden(t, "status-line", StatusLine{
		Mode:   "READY",
		Doc:    "click anything for its object menu · drag borders to resize",
		Counts: "6 tiles · 2 workspaces",
		Width:  900,
	}.Render())
}

func TestGoldenTopBar(t *testing.T) {
	checkGolden(t, "top-bar", TopBar{
		Workspaces: []string{"lab", "help"},
		Current:    0,
		Width:      900,
	}.Render())
}

func TestGoldenMenu(t *testing.T) {
	checkGolden(t, "menu", Menu{
		Header: "<color> #d3b56a",
		Items: []string{
			"Inspect",
			"Mix with…  (accept a color)",
			"Lighten (new swatch)",
			"Remove swatch",
			"Collect into Notes",
		},
		Hover: 1,
	}.Render())
}

func TestGoldenDropPreview(t *testing.T) {
	checkGolden(t, "drop-preview", DropPreview{W: 300, H: 200, Label: "= swap apps"}.Render())
}

func TestMenuHitTesting(t *testing.T) {
	m := Menu{Header: "<color> x", Items: []string{"a", "b", "c"}}
	if got := m.MenuItemAt(10, 18+BorderW+1); got != 0 {
		t.Errorf("first row hit = %d", got)
	}
	if got := m.MenuItemAt(10, 18+BorderW+MenuRowH+1); got != 1 {
		t.Errorf("second row hit = %d", got)
	}
	if got := m.MenuItemAt(10, 2); got != -1 {
		t.Errorf("header hit = %d, want -1", got)
	}
}
