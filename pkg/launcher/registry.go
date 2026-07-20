// Package launcher is the command registry behind every launcher
// surface (GGWM-008): one model of "what can be launched" — XDG desktop
// applications, WM builtins, script-registered commands — with fuzzy
// matching and frecency ordering. Pure Go: no X, no broker; surfaces
// and executors live elsewhere (L-D1: one registry, many surfaces).
package launcher

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind classifies how a command launches.
type Kind string

const (
	KindApp     Kind = "app"     // exec a process (XDG desktop entry)
	KindBuiltin Kind = "builtin" // set a leaf's app (WM-rendered tile)
	KindScript  Kind = "script"  // post to the owning JS runtime
)

// Command is one launchable entry.
type Command struct {
	ID       string   `json:"id"`    // "app:firefox", "builtin:trace", "script:deploy"
	Label    string   `json:"label"` // "Firefox"
	Exec     string   `json:"exec,omitempty"`
	Kind     Kind     `json:"kind"`
	Terminal bool     `json:"terminal,omitempty"` // wrap in the terminal command
	Keywords []string `json:"keywords,omitempty"`
	Doc      string   `json:"doc,omitempty"` // one-line description
	Src      string   `json:"src,omitempty"` // source .desktop path (apps)
}

// Scored is a match result.
type Scored struct {
	Command
	Score float64 `json:"score"`
}

// Registry aggregates the sources. Safe for concurrent use.
type Registry struct {
	mu       sync.Mutex
	dataDirs []string // XDG application dirs to scan
	apps     []Command
	static   map[Kind][]Command // builtins, script commands (caller-owned)
	scanned  map[string]time.Time
	frec     *frecency
	now      func() time.Time
}

// Option configures a Registry.
type Option func(*Registry)

// WithDataDirs overrides the XDG application directories (tests).
func WithDataDirs(dirs ...string) Option {
	return func(r *Registry) { r.dataDirs = dirs }
}

// WithStatePath overrides the frecency state file ("" disables persistence).
func WithStatePath(path string) Option {
	return func(r *Registry) { r.frec = newFrecency(path) }
}

// WithNow injects a clock (tests).
func WithNow(now func() time.Time) Option {
	return func(r *Registry) { r.now = now }
}

// New creates a registry. Call Refresh before the first query.
func New(opts ...Option) *Registry {
	r := &Registry{
		dataDirs: defaultDataDirs(),
		static:   map[Kind][]Command{},
		scanned:  map[string]time.Time{},
		now:      time.Now,
	}
	for _, o := range opts {
		o(r)
	}
	if r.frec == nil {
		r.frec = newFrecency(defaultStatePath())
	}
	r.frec.load()
	return r
}

// SetStatic replaces one kind's command list (builtins from the WM,
// script commands from the JS modules). Entries keep registration order
// among equals.
func (r *Registry) SetStatic(kind Kind, cmds []Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.static[kind] = append([]Command(nil), cmds...)
}

// Refresh rescans the desktop directories when their mtimes changed.
// Errors are per-directory and non-fatal (a missing dir is normal).
func (r *Registry) Refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	apps, scanned, changed := scanDesktopDirs(r.dataDirs, r.scanned)
	if changed {
		r.apps = apps
		r.scanned = scanned
	}
}

// Get finds a command by ID.
func (r *Registry) Get(id string) (Command, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.all() {
		if c.ID == id {
			return c, true
		}
	}
	return Command{}, false
}

// all returns every command (caller holds mu).
func (r *Registry) all() []Command {
	out := make([]Command, 0, len(r.apps)+8)
	out = append(out, r.static[KindBuiltin]...)
	out = append(out, r.static[KindScript]...)
	out = append(out, r.apps...)
	return out
}

// All returns every command, frecency-ordered (ties: label).
func (r *Registry) All() []Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.all()
	now := r.now()
	sort.SliceStable(out, func(i, j int) bool {
		fi, fj := r.frec.score(out[i].ID, now), r.frec.score(out[j].ID, now)
		if fi != fj {
			return fi > fj
		}
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

// Match fuzzy-matches query against every command; results are sorted
// by score (frecency-weighted) descending. Empty query = All() scored 0.
func (r *Registry) Match(query string) []Scored {
	if strings.TrimSpace(query) == "" {
		all := r.All()
		out := make([]Scored, len(all))
		for i, c := range all {
			out[i] = Scored{Command: c}
		}
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	var out []Scored
	for _, c := range r.all() {
		s, ok := scoreCommand(query, c)
		if !ok {
			continue
		}
		s *= 1 + log1p(r.frec.score(c.ID, now))
		out = append(out, Scored{Command: c, Score: s})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

// Bump records a launch for frecency. Persisting is best-effort and
// debounced inside the store.
func (r *Registry) Bump(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frec.bump(id, r.now())
}

// Flush persists any pending frecency state immediately and cancels the
// trailing debounced write. Call at shutdown so a burst of launches that
// never reached the 5s deadline is not lost (Codex review RC-9).
func (r *Registry) Flush() {
	r.mu.Lock()
	frec := r.frec
	r.mu.Unlock()
	frec.Flush()
}
