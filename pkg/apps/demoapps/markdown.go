package demoapps

import (
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// Markdown is a minimal markdown viewer. Its "Open…" button *accepts* a
// <file> — click a row in the file browser to load it, the flagship
// cross-app accept demo. It also owns the file.view verb, so "View as
// markdown" appears on every <file> menu system-wide.
type Markdown struct {
	Path  string
	Lines []string
	Err   string
	top   int // scroll offset (lines)
}

func NewMarkdown(path string) *Markdown {
	m := &Markdown{}
	if path != "" {
		m.load(path)
	}
	return m
}

func (m *Markdown) load(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.Err = err.Error()
		return
	}
	m.Path, m.Err, m.top = path, "", 0
	m.Lines = strings.Split(strings.ReplaceAll(string(data), "\t", "    "), "\n")
}

func (m *Markdown) Name() string  { return "demo-markdown" }
func (m *Markdown) Title() string { return "markdown viewer" }

func (m *Markdown) Verbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "file.view", Label: "View as markdown", Ptypes: []string{"file"}},
	}
}

func (m *Markdown) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	img := apps.NewSurface(w, h)
	var regions []apps.Region
	r := apps.Btn(img, 8, 8, "Open…  (accept a file)", draw.Blue)
	regions = append(regions, apps.Region{Rect: r, Action: "cmd:open",
		Doc: "accept a <file> — e.g. click a row in the file browser"})
	if m.Path != "" {
		obj, _ := pbui.NewObject("file", m.Path)
		obj.Label = filepath.Base(m.Path)
		chip := apps.Chip(img, r.Max.X+10, 10, obj,
			len(accepting) > 0 && pbui.TypeMatches(accepting, "file"))
		regions = append(regions, apps.Region{Rect: chip, Object: &obj, Doc: "<file> " + m.Path})
	}
	up := apps.Btn(img, w-84, 8, "up", draw.PaneAlt)
	dn := apps.Btn(img, w-44, 8, "dn", draw.PaneAlt)
	regions = append(regions,
		apps.Region{Rect: up, Action: "scroll:-10", Doc: "scroll up"},
		apps.Region{Rect: dn, Action: "scroll:+10", Doc: "scroll down"})

	if m.Err != "" {
		apps.Hint(img, 8, 52, m.Err)
		return img, regions
	}
	if m.Path == "" {
		apps.Hint(img, 8, 52, "no file loaded — Open… then click a <file> anywhere")
		return img, regions
	}

	y := 52
	inCode := false
	for i := m.top; i < len(m.Lines); i++ {
		if y > h-14 {
			break
		}
		line := m.Lines[i]
		switch {
		case strings.HasPrefix(line, "```"):
			inCode = !inCode
			y += 6
		case inCode:
			draw.Fill(img, image.Rect(8, y-2, w-8, y+13), draw.PaneAlt)
			draw.Text(img, 14, y+10, truncate(line, w/7), false, 10.5, draw.Ink)
			y += 15
		case strings.HasPrefix(line, "# "):
			draw.Text(img, 8, y+16, truncate(line[2:], w/9), true, 15, draw.Ink)
			tw := draw.TextWidth(truncate(line[2:], w/9), true, 15)
			draw.Fill(img, image.Rect(8, y+20, 8+tw, y+22), draw.Ink)
			y += 30
		case strings.HasPrefix(line, "## "):
			draw.Text(img, 8, y+14, truncate(line[3:], w/8), true, 13, draw.Ink)
			y += 24
		case strings.HasPrefix(line, "### "):
			draw.Text(img, 8, y+12, truncate(line[4:], w/7), true, 11.5, draw.Ink)
			y += 20
		case strings.HasPrefix(strings.TrimSpace(line), "- ") || strings.HasPrefix(strings.TrimSpace(line), "* "):
			draw.Fill(img, image.Rect(14, y+5, 18, y+9), draw.Ink)
			draw.Text(img, 26, y+11, truncate(strings.TrimSpace(line)[2:], w/7), false, 11, draw.Ink)
			y += 16
		case strings.TrimSpace(line) == "":
			y += 8
		default:
			draw.Text(img, 8, y+11, truncate(line, w/7), false, 11, draw.Ink)
			y += 16
		}
	}
	return img, regions
}

func truncate(s string, n int) string {
	if n < 8 {
		n = 8
	}
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (m *Markdown) HandleAction(ctx xapp.Ctx, action string) {
	switch {
	case action == "cmd:open":
		go func() {
			obj, err := ctx.Broker.Accept(contextBg(), []string{"file"},
				"OPEN — click a <file> presentation (file browser rows work) (Esc cancels)")
			if err != nil || obj == nil {
				return
			}
			ctx.Post(func() {
				m.load(obj.StringValue())
				ctx.Emit("markdown_opened", map[string]string{"path": m.Path})
				ctx.Redraw()
			})
		}()
	case strings.HasPrefix(action, "scroll:"):
		d := 10
		if action == "scroll:-10" {
			d = -10
		}
		m.top += d
		if m.top < 0 {
			m.top = 0
		}
		if m.top > len(m.Lines)-1 {
			m.top = len(m.Lines) - 1
		}
		ctx.Redraw()
	}
}

func (m *Markdown) HandleVerb(ctx xapp.Ctx, verbID string, obj *pbui.Object) {
	if verbID == "file.view" && obj != nil {
		m.load(obj.StringValue())
		ctx.Redraw()
	}
}
