package cmds

import (
	"context"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// QueryEventsCommand streams the broker's event bus as glazed rows.
type QueryEventsCommand struct {
	*glazed_cmds.CommandDescription
}

type queryEventsSettings struct {
	Socket string `glazed:"socket"`
	Count  int    `glazed:"count"`
}

func NewQueryEventsCommand() (*QueryEventsCommand, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	return &QueryEventsCommand{glazed_cmds.NewCommandDescription("events",
		glazed_cmds.WithShort("Follow the PBUI event bus (the trace)"),
		glazed_cmds.WithLong(`Subscribes to the broker's event bus and emits one row per event until
interrupted (or --count events have arrived). This is the Go form of the
prototype's trace app, and the assertion stream for tests.`),
		glazed_cmds.WithFlags(
			socketFlag(),
			fields.New("count", fields.TypeInteger, fields.WithDefault(0),
				fields.WithHelp("stop after N events (0 = follow forever)")),
		),
		glazed_cmds.WithSections(glazedSection),
	)}, nil
}

func (c *QueryEventsCommand) RunIntoGlazeProcessor(ctx context.Context, vals *values.Values, gp middlewares.Processor) error {
	s := &queryEventsSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	cl, err := client.Connect(ctx, client.Options{Socket: socketOrDefault(s.Socket), Name: "cli-events"})
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()
	events, err := cl.Events(ctx)
	if err != nil {
		return err
	}
	n := 0
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			row := types.NewRow(
				types.MRP("seq", ev.EventSeq),
				types.MRP("event", ev.Event),
				types.MRP("source", ev.Source),
				types.MRP("data", string(ev.Data)),
			)
			if err := gp.AddRow(ctx, row); err != nil {
				return err
			}
			n++
			if s.Count > 0 && n >= s.Count {
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// QueryVerbsCommand lists the broker's verb registry.
type QueryVerbsCommand struct {
	*glazed_cmds.CommandDescription
}

type queryVerbsSettings struct {
	Socket string `glazed:"socket"`
	Ptype  string `glazed:"ptype"`
}

func NewQueryVerbsCommand() (*QueryVerbsCommand, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	return &QueryVerbsCommand{glazed_cmds.NewCommandDescription("verbs",
		glazed_cmds.WithShort("List registered verbs (the type-directed action table)"),
		glazed_cmds.WithFlags(
			socketFlag(),
			fields.New("ptype", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("only verbs applicable to this ptype")),
		),
		glazed_cmds.WithSections(glazedSection),
	)}, nil
}

func (c *QueryVerbsCommand) RunIntoGlazeProcessor(ctx context.Context, vals *values.Values, gp middlewares.Processor) error {
	s := &queryVerbsSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	cl, err := client.Connect(ctx, client.Options{Socket: socketOrDefault(s.Socket), Name: "cli-verbs"})
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()
	verbs, err := cl.QueryVerbs(ctx, s.Ptype)
	if err != nil {
		return err
	}
	for _, v := range verbs {
		if err := gp.AddRow(ctx, types.NewRow(
			types.MRP("id", v.ID),
			types.MRP("label", v.Label),
			types.MRP("ptypes", v.Ptypes),
			types.MRP("accepts", v.Accepts),
			types.MRP("owner", v.Owner),
		)); err != nil {
			return err
		}
	}
	return nil
}
