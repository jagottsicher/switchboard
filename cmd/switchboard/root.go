package main

import (
	"github.com/spf13/cobra"
)

// version wird beim Release-Build per -ldflags "-X main.version=…" gesetzt.
var version = "dev"

// newRootCmd baut das Wurzelkommando. Die Unterkommandos kommen in späteren Schritten hinzu;
// ohne Unterkommando startet später die TUI.
func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switchboard",
		Short: "Leitstelle für Coding-Agenten in GNU-screen-Sessions",
		Long: "switchboard zeigt den Status von Agenten in lokalen und entfernten screen-Sessions,\n" +
			"schickt ihnen Eingaben und hängt sich bei Bedarf mit screen -x an.",
		Version:      version,
		SilenceUsage: true,
	}
}
