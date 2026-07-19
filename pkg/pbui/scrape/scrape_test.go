package scrape

import (
	"strings"
	"testing"
)

func TestLineWrapsMatches(t *testing.T) {
	out := Line("commit 8d6d02f fixed #b0563f in /etc/hosts", DefaultRules())
	for _, want := range []string{
		"\x1b]8;;pbui://git-commit/8d6d02f\x1b\\8d6d02f\x1b]8;;\x1b\\",
		"\x1b]8;;pbui://color/%23b0563f\x1b\\#b0563f\x1b]8;;\x1b\\",
		"\x1b]8;;pbui://file/%2Fetc%2Fhosts\x1b\\/etc/hosts\x1b]8;;\x1b\\",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestLineNoMatchesUnchanged(t *testing.T) {
	in := "nothing interesting here"
	if got := Line(in, DefaultRules()); got != in {
		t.Errorf("unchanged line was modified: %q", got)
	}
}

func TestColorWinsOverCommit(t *testing.T) {
	// "b0563f" inside "#b0563f" must not double-link as a git sha.
	out := Line("#b0563f", DefaultRules())
	if strings.Count(out, "\x1b]8;;pbui://") != 1 {
		t.Errorf("overlapping match double-linked: %q", out)
	}
	if !strings.Contains(out, "pbui://color/") {
		t.Errorf("color rule should win: %q", out)
	}
}

func TestRulesByName(t *testing.T) {
	rs := RulesByName([]string{"path"})
	if len(rs) != 1 || rs[0].Name != "path" {
		t.Fatalf("RulesByName: %+v", rs)
	}
}
