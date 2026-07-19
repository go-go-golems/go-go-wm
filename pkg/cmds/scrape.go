package cmds

import (
	"context"
	"os"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/scrape"
)

// ScrapeCommand is the stdin→stdout presentation-injecting filter.
type ScrapeCommand struct {
	*glazed_cmds.CommandDescription
}

type scrapeSettings struct {
	Rules []string `glazed:"rules"`
}

func NewScrapeCommand() (*ScrapeCommand, error) {
	return &ScrapeCommand{glazed_cmds.NewCommandDescription("scrape",
		glazed_cmds.WithShort("Inject pbui presentation links into plain text"),
		glazed_cmds.WithLong(`Reads stdin, wraps recognizable objects (hex colors, git SHAs, paths,
IPs, URLs) in OSC 8 pbui:// links, and writes stdout — the terminal analog
of CLIM presentation translators for non-cooperating programs.

Example:
  git log --oneline | go-go-wm scrape
  ls -la / | go-go-wm scrape --rules path`),
		glazed_cmds.WithFlags(
			fields.New("rules", fields.TypeStringList, fields.WithDefault([]string{}),
				fields.WithHelp("rule subset: color, git-commit, path, ip, url (default: all)")),
		),
	)}, nil
}

func (c *ScrapeCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &scrapeSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	return scrape.Filter(os.Stdin, os.Stdout, scrape.RulesByName(s.Rules))
}
