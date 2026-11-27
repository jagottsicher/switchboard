package main

import (
	"github.com/spf13/cobra"
)

// version is set for release builds via -ldflags "-X main.version=…".
var version = "dev"

// newRootCmd builds the root command with all subcommands. Without a subcommand, the TUI will
// start in a later version.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "switchboard",
		Short: "Control room for coding agents running in GNU screen sessions",
		Long: "switchboard shows the status of agents in local and remote screen sessions,\n" +
			"sends them input and attaches with screen -x when needed.",
		Version:      version,
		SilenceUsage: true,
	}
	root.AddCommand(newListCmd(), newPeekCmd(), newSendCmd(), newAttachCmd(), newNewCmd(),
		newHookCmd(), newNotifyCmd(), newHooksCmd())
	return root
}
