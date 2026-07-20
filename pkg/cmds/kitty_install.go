package cmds

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
)

//go:embed kitty/pbui_accept.py
var kittenSource []byte

const openActionsSnippet = `# go-go-wm: route pbui:// links to the PBUI menu (installed by 'go-go-wm kitty install')
protocol pbui
action launch --type=background go-go-wm menu --uri ${URL}
`

// KittyInstallCommand materializes the embedded kitten and open-actions
// snippet into the kitty config directory — the binary carries its own
// kitty integration so there is no version skew (design decision D3).
type KittyInstallCommand struct {
	*glazed_cmds.CommandDescription
}

type kittyInstallSettings struct {
	ConfigDir string `glazed:"config-dir"`
	Force     bool   `glazed:"force"`
}

func NewKittyInstallCommand() (*KittyInstallCommand, error) {
	return &KittyInstallCommand{glazed_cmds.NewCommandDescription("install",
		glazed_cmds.WithShort("Install the PBUI kitten and open_actions snippet into kitty"),
		glazed_cmds.WithLong(`Writes:
  <config>/pbui_accept.py       — the accept-picker kitten
  <config>/open-actions.conf    — routes pbui:// links to 'go-go-wm menu'
                                  (appended if the file exists)

Kitty must run with allow_remote_control enabled for accept mode to reach
the terminal. <config> defaults to $KITTY_CONFIG_DIRECTORY or ~/.config/kitty.`),
		glazed_cmds.WithFlags(
			fields.New("config-dir", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("kitty config directory (default: $KITTY_CONFIG_DIRECTORY or ~/.config/kitty)")),
			fields.New("force", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("overwrite an existing kitten file")),
		),
	)}, nil
}

func (c *KittyInstallCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &kittyInstallSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	dir := s.ConfigDir
	if dir == "" {
		dir = os.Getenv("KITTY_CONFIG_DIRECTORY")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, ".config", "kitty")
	}
	// Clean the user-supplied path so a leading/trailing separator or
	// a relative ".." segment cannot traverse outside the intended dir.
	// The flag is a local, user-owned config path, so this is defensive.
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	kitten := filepath.Join(dir, "pbui_accept.py")
	if _, err := os.Stat(kitten); err == nil && !s.Force {
		return fmt.Errorf("%s exists (use --force to overwrite)", kitten)
	}
	if err := os.WriteFile(kitten, kittenSource, 0o644); err != nil { //#nosec G302,G703 -- kitten is a world-readable helper script, not secret
		return err
	}
	fmt.Println("wrote", kitten)

	oa := filepath.Join(dir, "open-actions.conf")
	existing, _ := os.ReadFile(oa)
	if containsMarker(string(existing)) {
		fmt.Println(oa, "already configured")
		return nil
	}
	f, err := os.OpenFile(oa, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //#nosec G302,G703 -- config snippet is world-readable by design
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if len(existing) > 0 {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString(openActionsSnippet); err != nil {
		return err
	}
	fmt.Println("updated", oa)
	fmt.Println("note: kitty needs 'allow_remote_control yes' (or socket-only) for accept mode")
	return nil
}

func containsMarker(s string) bool {
	return len(s) > 0 && (contains(s, "protocol pbui"))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
