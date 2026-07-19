package launcher

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// XDG desktop-entry scanning. Deliberately not spec-complete (recorded
// in the design doc): field codes are stripped, DBusActivatable and
// desktop actions are ignored, no icon loading (L-D2).

// defaultDataDirs returns the XDG application directories.
func defaultDataDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local/share/applications"))
	}
	data := os.Getenv("XDG_DATA_DIRS")
	if data == "" {
		data = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(data, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	return dirs
}

// defaultStatePath returns the frecency file location.
func defaultStatePath() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		state = filepath.Join(home, ".local/state")
	}
	return filepath.Join(state, "go-go-wm", "launcher.json")
}

// scanDesktopDirs re-parses every dir whose mtime changed; returns the
// full app list, the new mtime map, and whether anything changed.
// Later dirs never shadow earlier ones (XDG precedence: first wins by
// desktop-file id).
func scanDesktopDirs(dirs []string, prev map[string]time.Time) ([]Command, map[string]time.Time, bool) {
	scanned := make(map[string]time.Time, len(dirs))
	changed := false
	for _, d := range dirs {
		st, err := os.Stat(d)
		if err != nil {
			scanned[d] = time.Time{}
			if _, had := prev[d]; !had || !prev[d].IsZero() {
				changed = true
			}
			continue
		}
		scanned[d] = st.ModTime()
		if prev[d] != st.ModTime() {
			changed = true
		}
	}
	if !changed && len(prev) == len(scanned) {
		return nil, scanned, false
	}

	seen := map[string]bool{} // desktop-file id → taken (first dir wins)
	var out []Command
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".desktop") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			id := strings.TrimSuffix(name, ".desktop")
			if seen[id] {
				continue
			}
			seen[id] = true
			if cmd, ok := parseDesktopFile(filepath.Join(d, name), id); ok {
				out = append(out, cmd)
			}
		}
	}
	return out, scanned, true
}

// parseDesktopFile reads one .desktop file's [Desktop Entry] group.
// Returns ok=false for hidden/NoDisplay/non-Application/exec-less
// entries.
func parseDesktopFile(path, id string) (Command, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Command{}, false
	}
	defer func() { _ = f.Close() }()

	cmd := Command{ID: "app:" + id, Kind: KindApp}
	typ := ""
	inEntry := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// Only the main group; [Desktop Action …] groups are ignored.
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "Type":
			typ = val
		case "Name":
			cmd.Label = val
		case "Comment":
			cmd.Doc = val
		case "Exec":
			cmd.Exec = stripFieldCodes(val)
		case "Terminal":
			cmd.Terminal = val == "true"
		case "NoDisplay", "Hidden":
			if val == "true" {
				return Command{}, false
			}
		case "Keywords", "Categories":
			for _, k := range strings.Split(val, ";") {
				if k = strings.TrimSpace(k); k != "" {
					cmd.Keywords = append(cmd.Keywords, k)
				}
			}
		}
	}
	if typ != "" && typ != "Application" {
		return Command{}, false
	}
	if cmd.Exec == "" {
		return Command{}, false
	}
	if cmd.Label == "" {
		cmd.Label = id
	}
	return cmd, true
}

// stripFieldCodes removes the %f/%u/%F/%U/%i/%c/%k placeholders (the
// launcher never passes files) and collapses the leftover whitespace.
func stripFieldCodes(exec string) string {
	fields := strings.Fields(exec)
	out := fields[:0]
	for _, f := range fields {
		if len(f) == 2 && f[0] == '%' {
			switch f[1] {
			case 'f', 'u', 'F', 'U', 'i', 'c', 'k', 'd', 'D', 'n', 'N', 'v', 'm':
				continue
			}
		}
		out = append(out, strings.ReplaceAll(f, "%%", "%"))
	}
	return strings.Join(out, " ")
}
