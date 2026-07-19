// Package scrape turns plain text into presentations: a rule table of
// regexes mapping matches to ptypes, and a stdin→stdout filter that wraps
// matches in OSC 8 pbui links. It is the terminal analog of CLIM's
// presentation translators for non-cooperating programs.
package scrape

import (
	"bufio"
	"io"
	"regexp"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/present"
)

// Rule maps a regex to a ptype.
type Rule struct {
	Name  string
	Ptype string
	Re    *regexp.Regexp
}

// DefaultRules cover the classics: hex colors, git SHAs, absolute paths,
// IPs, PIDs-after-keyword. Order matters: earlier rules win on overlap.
func DefaultRules() []Rule {
	return []Rule{
		{"color", "color", regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)},
		{"git-commit", "git-commit", regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)},
		{"path", "file", regexp.MustCompile(`(?:^|\s)(/[A-Za-z0-9._\-/]{2,})`)},
		{"ip", "ip", regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)},
		{"url", "url", regexp.MustCompile(`https?://[^\s]+`)},
	}
}

// RulesByName filters DefaultRules to the named subset (empty = all).
func RulesByName(names []string) []Rule {
	all := DefaultRules()
	if len(names) == 0 {
		return all
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []Rule
	for _, r := range all {
		if want[r.Name] {
			out = append(out, r)
		}
	}
	return out
}

// Line wraps every rule match in line with an OSC 8 pbui link and returns
// the annotated line. Overlapping matches keep the earliest/leftmost.
func Line(line string, rules []Rule) string {
	type span struct {
		start, end int
		ptype      string
	}
	var spans []span
	taken := make([]bool, len(line))
	for _, r := range rules {
		for _, m := range r.Re.FindAllStringSubmatchIndex(line, -1) {
			start, end := m[0], m[1]
			// Rules with a capture group link only the group.
			if len(m) >= 4 && m[2] >= 0 {
				start, end = m[2], m[3]
			}
			overlap := false
			for i := start; i < end; i++ {
				if taken[i] {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}
			for i := start; i < end; i++ {
				taken[i] = true
			}
			spans = append(spans, span{start, end, r.Ptype})
		}
	}
	if len(spans) == 0 {
		return line
	}
	// Render left to right.
	ordered := make([]span, 0, len(spans))
	for i := 0; i < len(line); {
		found := false
		for _, s := range spans {
			if s.start == i {
				ordered = append(ordered, s)
				i = s.end
				found = true
				break
			}
		}
		if !found {
			i++
		}
	}
	out := ""
	pos := 0
	for _, s := range ordered {
		out += line[pos:s.start]
		obj, err := pbui.NewObject(s.ptype, line[s.start:s.end])
		if err != nil {
			out += line[s.start:s.end]
		} else {
			out += present.Link(obj, line[s.start:s.end])
		}
		pos = s.end
	}
	out += line[pos:]
	return out
}

// Filter annotates r line-by-line into w.
func Filter(r io.Reader, w io.Writer, rules []Rule) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	bw := bufio.NewWriter(w)
	defer func() { _ = bw.Flush() }()
	for sc.Scan() {
		if _, err := bw.WriteString(Line(sc.Text(), rules) + "\n"); err != nil {
			return err
		}
	}
	return sc.Err()
}
