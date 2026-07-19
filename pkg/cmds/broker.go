// Package cmds defines the glazed command surface of the go-go-wm binary.
// One file per command; query commands are GlazeCommands (structured
// output), everything else is a BareCommand.
package cmds

import (
	"context"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/broker"
)

func socketFlag() *fields.Definition {
	return fields.New("socket", fields.TypeString,
		fields.WithDefault(""),
		fields.WithHelp("broker socket path (default: $PBUI_SOCKET or $XDG_RUNTIME_DIR/pbui.sock)"))
}

func socketOrDefault(s string) string {
	if s != "" {
		return s
	}
	return broker.DefaultSocketPath()
}

// BrokerCommand runs the PBUI broker standalone.
type BrokerCommand struct {
	*glazed_cmds.CommandDescription
}

type brokerSettings struct {
	Socket string `glazed:"socket"`
}

func NewBrokerCommand() (*BrokerCommand, error) {
	return &BrokerCommand{glazed_cmds.NewCommandDescription("broker",
		glazed_cmds.WithShort("Run the PBUI broker daemon"),
		glazed_cmds.WithLong(`Runs the presentation broker on a Unix socket.

The broker owns the accept state machine, the verb registry, and the event
bus. It never touches X: the window manager, kitty kittens, and CLI helpers
are all just clients. Usually you let 'go-go-wm wm --embedded-broker' run it
in-process; run it standalone for headless work and tests.`),
		glazed_cmds.WithFlags(socketFlag()),
	)}, nil
}

func (c *BrokerCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &brokerSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	b := broker.New()
	log.Info().Str("socket", socketOrDefault(s.Socket)).Msg("broker listening")
	return b.ListenAndServe(ctx, socketOrDefault(s.Socket))
}
