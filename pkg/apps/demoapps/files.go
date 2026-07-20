package demoapps

import (
	"image"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// Files is the file browser: directory entries are <file> / <directory>
// presentations. Left click on a directory navigates (primary action);
// right click always menus; files answer pending <file> accepts — which is
// how the markdown viewer's "Open…" gets its input.
type Files struct {
	Dir     string
	Entries []fileEntry
	Err     string
}

type fileEntry struct {
	name  string
	isDir bool
	size  int64
}

func NewFiles(dir string) *Files {
	f := &Files{}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	f.load(dir)
	return f
}

func (f *Files) load(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		f.Err = err.Error()
		return
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		f.Err = err.Error()
		return
	}
	f.Dir, f.Err = abs, ""
	f.Entries = f.Entries[:0]
	for _, e := range ents {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			continue
		}
		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		f.Entries = append(f.Entries, fileEntry{name: e.Name(), isDir: e.IsDir(), size: size})
	}
	sort.Slice(f.Entries, func(i, j int) bool {
		if f.Entries[i].isDir != f.Entries[j].isDir {
			return f.Entries[i].isDir
		}
		return f.Entries[i].name < f.Entries[j].name
	})
}

func (f *Files) Name() string  { return "demo-files" }
func (f *Files) Title() string { return "file browser" }

func (f *Files) Verbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "dir.browse", Label: "Browse here", Ptypes: []string{"directory"}},
		{ID: "file.head", Label: "Print first lines to listener", Ptypes: []string{"file"}},
	}
}

func (f *Files) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	img := apps.NewSurface(w, h)
	var regions []apps.Region
	draw.Text(img, 8, 18, f.Dir, true, 11, draw.Current().Ink)
	if f.Err != "" {
		apps.Hint(img, 8, 36, f.Err)
		return img, regions
	}
	y := 30
	rowH := 19
	// Parent entry.
	parent := filepath.Dir(f.Dir)
	rows := append([]fileEntry{{name: "..", isDir: true}}, f.Entries...)
	for _, e := range rows {
		if y+rowH > h-4 {
			apps.Hint(img, 8, y+12, "…")
			break
		}
		full := filepath.Join(f.Dir, e.name)
		if e.name == ".." {
			full = parent
		}
		ptype := "file"
		label := e.name
		if e.isDir {
			ptype = "directory"
			label += "/"
		}
		hl := len(accepting) > 0 && pbui.TypeMatches(accepting, ptype)
		r := image.Rect(8, y, w-8, y+rowH-1)
		if hl {
			draw.Fill(img, r, draw.Current().Sel)
			draw.Border(img, r, 1, draw.Current().Red)
		}
		tone := draw.Current().Ink
		bold := e.isDir
		draw.Text(img, 12, y+13, label, bold, 11, tone)
		if !e.isDir {
			draw.Text(img, w-90, y+13, byteSize(e.size), false, 9.5, draw.Current().Faint)
		}
		obj, _ := pbui.NewObject(ptype, full)
		obj.Label = e.name
		region := apps.Region{Rect: r, Object: &obj,
			Doc: "<" + ptype + "> " + full}
		if e.isDir {
			region.Action = "nav:" + full
			region.Doc += " — L: browse · R: menu"
		}
		regions = append(regions, region)
		y += rowH
	}
	return img, regions
}

func byteSize(n int64) string {
	switch {
	case n > 1<<20:
		return itoa64(n>>20) + "M"
	case n > 1<<10:
		return itoa64(n>>10) + "K"
	default:
		return itoa64(n)
	}
}

func itoa64(n int64) string { return intToStr(int(n)) }

func (f *Files) HandleAction(ctx xapp.Ctx, action string) {
	if len(action) > 4 && action[:4] == "nav:" {
		f.load(action[4:])
		ctx.Emit("dir_browsed", map[string]string{"dir": f.Dir})
		ctx.Redraw()
	}
}

func (f *Files) HandleVerb(ctx xapp.Ctx, verbID string, obj *pbui.Object) {
	if obj == nil {
		return
	}
	path := obj.StringValue()
	switch verbID {
	case "dir.browse":
		f.load(path)
		ctx.Redraw()
	case "file.head":
		data, err := os.ReadFile(path)
		if err != nil {
			ctx.Print(apps.Seg{Text: "read error: " + err.Error()})
			return
		}
		lines := splitLines(string(data), 3)
		fo, _ := pbui.NewObject("file", path)
		fo.Label = filepath.Base(path)
		ctx.Print(apps.Seg{Object: &fo}, apps.Seg{Text: ": " + lines})
	}
}

func splitLines(s string, n int) string {
	out := ""
	count := 0
	for i := 0; i < len(s) && count < n; i++ {
		if s[i] == '\n' {
			count++
			out += " ⏎ "
			continue
		}
		if len(out) > 160 {
			break
		}
		out += string(s[i])
	}
	return out
}
