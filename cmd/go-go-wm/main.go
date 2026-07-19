package main

import (
	"fmt"
	"os"

	"github.com/go-go-golems/glazed/pkg/cli"
	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	wmcmds "github.com/go-go-golems/go-go-wm/pkg/cmds"
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "go-go-wm",
	Short:   "go-go-wm — a PBUI window manager: tiles, workspaces, presentations",
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return logging.InitLoggerFromCobra(cmd)
	},
}

func addBare(parent *cobra.Command, c glazed_cmds.Command, err error) {
	cobra.CheckErr(err)
	cc, err := cli.BuildCobraCommand(c,
		cli.WithParserConfig(cli.CobraParserConfig{AppName: "go-go-wm"}),
	)
	cobra.CheckErr(err)
	parent.AddCommand(cc)
}

func main() {
	err := logging.AddLoggingSectionToRootCommand(rootCmd, "go-go-wm")
	cobra.CheckErr(err)

	helpSystem := help.NewHelpSystem()
	help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

	// Daemons.
	brokerCmd, err := wmcmds.NewBrokerCommand()
	addBare(rootCmd, brokerCmd, err)
	wmCmd, err := wmcmds.NewWMCommand()
	addBare(rootCmd, wmCmd, err)

	// Participation CLI.
	acceptCmd, err := wmcmds.NewAcceptCommand()
	addBare(rootCmd, acceptCmd, err)
	answerCmd, err := wmcmds.NewAnswerCommand()
	addBare(rootCmd, answerCmd, err)
	presentCmd, err := wmcmds.NewPresentCommand()
	addBare(rootCmd, presentCmd, err)
	menuCmd, err := wmcmds.NewMenuCommand()
	addBare(rootCmd, menuCmd, err)
	scrapeCmd, err := wmcmds.NewScrapeCommand()
	addBare(rootCmd, scrapeCmd, err)
	runCmd, err := wmcmds.NewRunCommand()
	addBare(rootCmd, runCmd, err)
	demoCmd, err := wmcmds.NewDemoCommand()
	addBare(rootCmd, demoCmd, err)

	// Introspection.
	queryCmd := &cobra.Command{Use: "query", Short: "Ask the WM and broker what they believe"}
	rootCmd.AddCommand(queryCmd)
	queryEvents, err := wmcmds.NewQueryEventsCommand()
	addBare(queryCmd, queryEvents, err)
	queryVerbs, err := wmcmds.NewQueryVerbsCommand()
	addBare(queryCmd, queryVerbs, err)
	queryTree, err := wmcmds.NewQueryTreeCommand()
	addBare(queryCmd, queryTree, err)
	queryWindows, err := wmcmds.NewQueryWindowsCommand()
	addBare(queryCmd, queryWindows, err)

	// Kitty integration.
	kittyCmd := &cobra.Command{Use: "kitty", Short: "Kitty terminal integration"}
	rootCmd.AddCommand(kittyCmd)
	kittyInstall, err := wmcmds.NewKittyInstallCommand()
	addBare(kittyCmd, kittyInstall, err)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
