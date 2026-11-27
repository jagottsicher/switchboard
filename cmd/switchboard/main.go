// Command switchboard is a control room for coding agents running in GNU screen sessions.
package main

import (
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
