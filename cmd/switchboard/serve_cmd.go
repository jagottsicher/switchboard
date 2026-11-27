package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/jagottsicher/switchboard/internal/host"
)

func newServeCmd() *cobra.Command {
	var stdio bool
	cmd := &cobra.Command{
		Use:   "serve --stdio",
		Short: "Answer requests of another switchboard (started over ssh)",
		Long: "Answers requests of a switchboard on another machine over standard input and\n" +
			"output. The other switchboard starts this through ssh; there is no need to run it\n" +
			"by hand.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !stdio {
				return errors.New("serve needs --stdio")
			}
			return host.Serve(cmd.InOrStdin(), cmd.OutOrStdout(), host.NewLocal(), version)
		},
	}
	cmd.Flags().BoolVar(&stdio, "stdio", false, "talk over standard input and output")
	return cmd
}
