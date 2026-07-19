package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Frecency: count weighted by recency buckets. Losing the state file
// costs nothing but ordering, so every disk operation is best-effort
// and writes are debounced.

type frecEntry struct {
	Count    int       `json:"count"`
	LastUsed time.Time `json:"last_used"`
}

type frecency struct {
	path     string // "" = in-memory only
	entries  map[string]frecEntry
	lastSave time.Time
}

func newFrecency(path string) *frecency {
	return &frecency{path: path, entries: map[string]frecEntry{}}
}

func (f *frecency) load() {
	if f.path == "" {
		return
	}
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, &f.entries)
}

// bump records a use and saves (debounced to one write per 5s burst).
func (f *frecency) bump(id string, now time.Time) {
	e := f.entries[id]
	e.Count++
	e.LastUsed = now
	f.entries[id] = e
	if f.path == "" {
		return
	}
	if now.Sub(f.lastSave) < 5*time.Second {
		return
	}
	f.lastSave = now
	f.save()
}

func (f *frecency) save() {
	if f.path == "" {
		return
	}
	raw, err := json.MarshalIndent(f.entries, "", " ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(f.path), 0o755)
	_ = os.WriteFile(f.path, raw, 0o644)
}

// score is the classic bucketed frecency: count × recency weight.
func (f *frecency) score(id string, now time.Time) float64 {
	e, ok := f.entries[id]
	if !ok || e.Count == 0 {
		return 0
	}
	age := now.Sub(e.LastUsed)
	w := 0.5
	switch {
	case age < time.Hour:
		w = 4
	case age < 24*time.Hour:
		w = 2
	case age < 7*24*time.Hour:
		w = 1
	}
	return float64(e.Count) * w
}
