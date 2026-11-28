package main

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jagottsicher/switchboard/internal/config"
	"github.com/jagottsicher/switchboard/internal/host"
)

func newHostsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hosts",
		Short: "Check the configured hosts",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Connect to every configured host and list its sessions",
		Long: "Connects to every host in the configuration file through ssh, starts switchboard\n" +
			"there and lists its sessions. It reports for each host whether this works.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(cfg.Hosts) == 0 {
				path, _ := config.Path()
				fmt.Fprintf(out, "No hosts configured in %s.\n", path)
				return nil
			}
			hosts := host.FromConfig(cfg)
			defer closeAll(hosts)
			results := make([]string, len(hosts))
			failed := false
			for i, h := range hosts {
				infos, warn, err := h.List()
				switch {
				case err != nil:
					results[i], failed = "error: "+strings.TrimPrefix(err.Error(), h.Name()+": "), true
				case warn != nil:
					results[i] = fmt.Sprintf("ok, %d sessions (warning: %v)", len(infos), warn)
				default:
					results[i] = fmt.Sprintf("ok, %d sessions", len(infos))
				}
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for i, h := range cfg.Hosts {
				fmt.Fprintf(tw, "%s\t%s\n", h.Name, results[i])
			}
			tw.Flush()
			if failed {
				return errors.New("some hosts cannot be reached")
			}
			return nil
		},
	})
	return cmd
}
