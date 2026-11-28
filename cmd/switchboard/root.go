package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/jagottsicher/switchboard/internal/tui"
)

const (
	longName  = "switchboard"
	shortName = "swb"
)

// progName returns the name to show in help and usage: shortName when the binary is called
// through a link or copy named swb, otherwise longName.
func progName(arg0 string) string {
	if filepath.Base(arg0) == shortName {
		return shortName
	}
	return longName
}

// runTUI starts the interactive overview. Swappable for tests.
var runTUI = tui.Run

// isTerminal reports whether f is a terminal. Swappable for tests.
var isTerminal = func(f *os.File) bool {
	return term.IsTerminal(f.Fd())
}

// newRootCmd builds the root command with all subcommands. Without a subcommand, it starts the
// interactive overview.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   progName(os.Args[0]),
		Short: "Control room for coding agents running in GNU screen sessions",
		Long: "switchboard shows the status of agents in local and remote screen sessions,\n" +
			"sends them input and attaches with screen -x when needed.\n\n" +
			"Without a command, it starts the interactive overview.\n" +
			"swb is the short name: link or copy the binary as swb to use it.",
		Version:      versionInfo(),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
				return errors.New("the interactive overview needs a terminal " +
					"(use ssh -t for remote hosts); for scripts, use a command such as list")
			}
			return runTUI()
		},
	}
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	root.AddCommand(newListCmd(), newPeekCmd(), newSendCmd(), newAnswerCmd(), newAttachCmd(), newNewCmd(),
		newHookCmd(), newStatusLineCmd(), newNotifyCmd(), newHooksCmd(), newHostsCmd(), newServeCmd())
	return root
}
