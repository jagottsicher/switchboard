package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// version is set for release builds via -ldflags "-X main.version=…".
var version = "dev"

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

// newRootCmd builds the root command with all subcommands. Without a subcommand, the TUI will
// start in a later version.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   progName(os.Args[0]),
		Short: "Control room for coding agents running in GNU screen sessions",
		Long: "switchboard shows the status of agents in local and remote screen sessions,\n" +
			"sends them input and attaches with screen -x when needed.\n\n" +
			"swb is the short name: link or copy the binary as swb to use it.",
		Version:      version,
		SilenceUsage: true,
	}
	root.AddCommand(newListCmd(), newPeekCmd(), newSendCmd(), newAttachCmd(), newNewCmd(),
		newHookCmd(), newNotifyCmd(), newHooksCmd())
	return root
}
