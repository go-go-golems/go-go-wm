package launcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeDesktop(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeDesktop(t, dir, "firefox.desktop", `[Desktop Entry]
Type=Application
Name=Firefox
Comment=Browse the web
Exec=firefox %u
Keywords=browser;internet;
Categories=Network;WebBrowser;
`)
	writeDesktop(t, dir, "gcalc.desktop", `[Desktop Entry]
Type=Application
Name=Galculator
Exec=galculator
Terminal=false
`)
	writeDesktop(t, dir, "htop.desktop", `[Desktop Entry]
Type=Application
Name=htop
Exec=htop
Terminal=true
`)
	writeDesktop(t, dir, "hidden.desktop", `[Desktop Entry]
Type=Application
Name=Secret
Exec=secret
NoDisplay=true
`)
	writeDesktop(t, dir, "link.desktop", `[Desktop Entry]
Type=Link
Name=Some Link
URL=https://example.com
`)
	writeDesktop(t, dir, "actions.desktop", `[Desktop Entry]
Type=Application
Name=Multi
Exec=multi %F --flag %%p
[Desktop Action new-window]
Name=New Window
Exec=multi --new-window
`)
	return dir
}

func newTestRegistry(t *testing.T, dirs ...string) *Registry {
	t.Helper()
	r := New(WithDataDirs(dirs...), WithStatePath(""))
	r.Refresh()
	return r
}

func TestDesktopParsing(t *testing.T) {
	r := newTestRegistry(t, fixtureDir(t))
	all := r.All()
	byID := map[string]Command{}
	for _, c := range all {
		byID[c.ID] = c
	}
	if len(all) != 4 {
		t.Fatalf("want 4 visible apps, got %d: %+v", len(all), all)
	}
	ff := byID["app:firefox"]
	if ff.Label != "Firefox" || ff.Exec != "firefox" || ff.Doc != "Browse the web" {
		t.Fatalf("firefox parsed wrong: %+v", ff)
	}
	if len(ff.Keywords) != 4 { // browser, internet, Network, WebBrowser
		t.Fatalf("firefox keywords: %v", ff.Keywords)
	}
	if !byID["app:htop"].Terminal {
		t.Fatal("htop must be Terminal=true")
	}
	if _, hidden := byID["app:hidden"]; hidden {
		t.Fatal("NoDisplay entry leaked")
	}
	if _, link := byID["app:link"]; link {
		t.Fatal("non-Application entry leaked")
	}
	// Field codes stripped, %% unescaped, action group ignored.
	if got := byID["app:actions"].Exec; got != "multi --flag %p" {
		t.Fatalf("field-code stripping: %q", got)
	}
}

func TestXDGPrecedenceFirstDirWins(t *testing.T) {
	local := t.TempDir()
	system := t.TempDir()
	writeDesktop(t, local, "firefox.desktop", `[Desktop Entry]
Type=Application
Name=Firefox Local
Exec=firefox-local
`)
	writeDesktop(t, system, "firefox.desktop", `[Desktop Entry]
Type=Application
Name=Firefox System
Exec=firefox-system
`)
	r := newTestRegistry(t, local, system)
	c, ok := r.Get("app:firefox")
	if !ok || c.Label != "Firefox Local" {
		t.Fatalf("first dir must win: %+v", c)
	}
}

func TestRefreshMtimeCheck(t *testing.T) {
	dir := t.TempDir()
	writeDesktop(t, dir, "one.desktop", "[Desktop Entry]\nType=Application\nName=One\nExec=one\n")
	r := newTestRegistry(t, dir)
	if len(r.All()) != 1 {
		t.Fatalf("initial scan: %d", len(r.All()))
	}
	writeDesktop(t, dir, "two.desktop", "[Desktop Entry]\nType=Application\nName=Two\nExec=two\n")
	// Force a visible mtime change even on coarse filesystems.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(dir, future, future); err != nil {
		t.Fatal(err)
	}
	r.Refresh()
	if len(r.All()) != 2 {
		t.Fatalf("after refresh: %d", len(r.All()))
	}
}

func TestSubsequenceScoring(t *testing.T) {
	cases := []struct {
		name        string
		query, a, b string // expect score(a) > score(b)
	}{
		{"boundary beats middle", "f", "firefox", "gftw"},
		{"consecutive run beats scattered", "fire", "firefox", "farmhire"},
		{"early match beats late", "top", "top", "desktop-help-top"},
		{"word boundary bonus", "wb", "web-browser", "awxby"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sa, oka := subsequenceScore(tc.query, tc.a)
			sb, okb := subsequenceScore(tc.query, tc.b)
			if !oka || !okb {
				t.Fatalf("both must match: %v %v", oka, okb)
			}
			if sa <= sb {
				t.Fatalf("score(%q,%q)=%v must beat score(%q,%q)=%v",
					tc.query, tc.a, sa, tc.query, tc.b, sb)
			}
		})
	}
	if _, ok := subsequenceScore("xyz", "firefox"); ok {
		t.Fatal("non-subsequence must not match")
	}
	if _, ok := subsequenceScore("", "firefox"); ok {
		t.Fatal("empty query is handled by Match, not the scorer")
	}
}

func TestMatchOrderingAndFields(t *testing.T) {
	r := newTestRegistry(t, fixtureDir(t))
	got := r.Match("fire")
	if len(got) == 0 || got[0].ID != "app:firefox" {
		t.Fatalf("fire → firefox first, got %+v", got)
	}
	// Keyword match: "browser" is a firefox keyword.
	got = r.Match("browser")
	if len(got) == 0 || got[0].ID != "app:firefox" {
		t.Fatalf("browser → firefox via keyword, got %+v", got)
	}
	// No match → empty.
	if got = r.Match("zzzzqq"); len(got) != 0 {
		t.Fatalf("no-match must be empty: %+v", got)
	}
}

func TestFrecencyOrdersAllAndBoostsMatch(t *testing.T) {
	base := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	clock := base
	r := New(WithDataDirs(fixtureDir(t)), WithStatePath(""),
		WithNow(func() time.Time { return clock }))
	r.Refresh()

	// Bump htop twice; All() must lead with it.
	r.Bump("app:htop")
	r.Bump("app:htop")
	if all := r.All(); all[0].ID != "app:htop" {
		t.Fatalf("All must lead with the bumped command, got %s", all[0].ID)
	}
	// A week later the boost decays but count persists.
	clock = base.Add(8 * 24 * time.Hour)
	if s := r.Match("htop"); len(s) == 0 || s[0].ID != "app:htop" {
		t.Fatalf("decayed frecency still matches: %+v", s)
	}
}

func TestStaticSources(t *testing.T) {
	r := newTestRegistry(t, t.TempDir())
	r.SetStatic(KindBuiltin, []Command{
		{ID: "builtin:trace", Label: "trace", Kind: KindBuiltin},
	})
	r.SetStatic(KindScript, []Command{
		{ID: "script:deploy", Label: "deploy prod", Kind: KindScript, Doc: "ship it"},
	})
	if _, ok := r.Get("builtin:trace"); !ok {
		t.Fatal("builtin source missing")
	}
	if got := r.Match("deploy"); len(got) != 1 || got[0].Kind != KindScript {
		t.Fatalf("script command must match: %+v", got)
	}
	// Replacing the script list drops stale entries (daemon death).
	r.SetStatic(KindScript, nil)
	if _, ok := r.Get("script:deploy"); ok {
		t.Fatal("stale script command survived SetStatic(nil)")
	}
}

func TestFrecencyPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "launcher.json")
	clock := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	r := New(WithDataDirs(fixtureDir(t)), WithStatePath(path),
		WithNow(func() time.Time { return clock }))
	r.Refresh()
	r.Bump("app:htop") // schedules a debounced write
	r.Flush()          // force the trailing write before checking the file
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}
	r2 := New(WithDataDirs(fixtureDir(t)), WithStatePath(path),
		WithNow(func() time.Time { return clock }))
	r2.Refresh()
	if all := r2.All(); all[0].ID != "app:htop" {
		t.Fatalf("persisted frecency must survive reload, got %s", all[0].ID)
	}
}

// TestFrecencyBurstFlushedOnShutdown is the regression test for Codex review
// RC-9: before the fix, bump() within 5s of a prior save dropped the trailing
// write entirely (it only checked "5s since lastSave" and returned), so a
// burst of launches right before shutdown lost all its counts. Now bump()
// (re)arms a resettable timer and Flush() forces the write, so a burst is
// always persisted.
func TestFrecencyBurstFlushedOnShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "launcher.json")
	clock := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	r := New(WithDataDirs(fixtureDir(t)), WithStatePath(path),
		WithNow(func() time.Time { return clock }))
	r.Refresh()
	// A burst: several bumps within the debounce window. With the old
	// "5s since lastSave" logic only the first would write; here the
	// trailing write is deferred and only lands on Flush.
	r.Bump("app:htop")
	r.Bump("app:htop")
	r.Bump("app:htop")
	// No file yet (write is debounced, not immediate).
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("state file should not exist before Flush")
	}
	// Flush at shutdown: the full burst (count 3) must persist.
	r.Flush()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written after Flush: %v", err)
	}
	r2 := New(WithDataDirs(fixtureDir(t)), WithStatePath(path),
		WithNow(func() time.Time { return clock }))
	r2.Refresh()
	all := r2.All()
	if all[0].ID != "app:htop" {
		t.Fatalf("burst must persist and order htop first, got %s", all[0].ID)
	}
}
