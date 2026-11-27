// Command switchboard ist eine Leitstelle für Coding-Agenten in GNU-screen-Sessions.
package main

import (
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
