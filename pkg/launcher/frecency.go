package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Frecency: count weighted by recency buckets. Losing the state file
// costs nothing but ordering, so every disk operation is best-effort.
// Writes are debounced via a resettable timer: every bump (re)schedules a
// single write 5s later, so a burst of launches always produces exactly one
// trailing write rather than dropping the last batch (Codex review RC-9).

type frecEntry struct {
	Count    int       `json:"count"`
	LastUsed time.Time `json:"last_used"`
}

type frecency struct {
	path string // "" = in-memory only

	mu       sync.Mutex
	entries  map[string]frecEntry
	now      func() time.Time
	timer    *time.Timer
	saving   bool
	lastSave time.Time
}

func newFrecency(path string) *frecency {
	return &frecency{path: path, entries: map[string]frecEntry{}, now: time.Now}
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

// bump records a use and schedules a debounced save. The save is a
// resettable timer rather than a "5s since last save" check: that older
// form dropped the timer on every launch within the window, so a burst
// ending right before shutdown lost all its counts.
func (f *frecency) bump(id string, now time.Time) {
	f.mu.Lock()
	e := f.entries[id]
	e.Count++
	e.LastUsed = now
	f.entries[id] = e
	disk := f.path != ""
	if disk {
		// (Re)schedule a single write 5s out. The first bump of a burst
		// arms it; subsequent bumps reset it so the write lands 5s after
		// the LAST launch — guaranteeing the trailing batch is persisted.
		if f.timer == nil {
			f.timer = time.AfterFunc(5*time.Second, f.save)
		} else {
			f.timer.Reset(5 * time.Second)
		}
	}
	f.mu.Unlock()
}

// Flush writes any pending state immediately and cancels the trailing
// timer. Safe to call at shutdown so a burst that never reached the 5s
// deadline is not lost. If a save is already in flight, Flush waits for it
// to finish and then persists again, so entries added after that save's
// snapshot are not lost (Codex review RC-16).
func (f *frecency) Flush() {
	f.mu.Lock()
	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}
	disk := f.path != ""
	f.mu.Unlock()
	if !disk {
		return
	}
	// Wait for any in-flight save to complete, then do a final save that
	// captures any entries added after the in-flight snapshot.
	f.waitSaveDone()
	f.save()
}

// waitSaveDone blocks until no save is in flight. save() sets saving=true
// before unlocking for disk I/O and clears it after; Flush uses this to
// avoid returning before the active write has settled.
func (f *frecency) waitSaveDone() {
	f.mu.Lock()
	for f.saving {
		// Spin briefly under the lock is acceptable: save's disk write is
		// fast and Flush runs once at shutdown. Avoid a condvar to keep the
		// store tiny.
		f.mu.Unlock()
		time.Sleep(time.Millisecond)
		f.mu.Lock()
	}
	f.mu.Unlock()
}

func (f *frecency) save() {
	if f.path == "" {
		return
	}
	f.mu.Lock()
	if f.saving {
		f.mu.Unlock()
		return
	}
	f.saving = true
	raw, err := json.MarshalIndent(f.entries, "", " ")
	f.lastSave = f.now()
	f.mu.Unlock()
	if err != nil {
		f.mu.Lock()
		f.saving = false
		f.mu.Unlock()
		return
	}
	_ = os.MkdirAll(filepath.Dir(f.path), 0o755)
	_ = os.WriteFile(f.path, raw, 0o644)
	f.mu.Lock()
	f.saving = false
	f.timer = nil
	f.mu.Unlock()
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
