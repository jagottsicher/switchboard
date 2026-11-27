package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/screen"
)

func newAnswerCmd() *cobra.Command {
	var window int
	cmd := &cobra.Command{
		Use:   "answer SESSION [CHOICE...]",
		Short: "Pick a choice in the dialog an agent shows",
		Long: "Picks the choice labeled CHOICE in the dialog the agent shows, such as a permission\n" +
			"prompt or a question. CHOICE is the whole label, for example \"No\"; case does not\n" +
			"matter. Numbers are not accepted, because they differ between dialogs. Without CHOICE,\n" +
			"the question and its choices are printed and nothing is sent.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, window, err := findWithWindow(cmd, args[0], window)
			if err != nil {
				return err
			}
			if s.State == screen.StateDead {
				return fmt.Errorf("%s is dead; screen -wipe removes it", s.ID)
			}
			d, err := answer.Read(s.ID, window)
			if err != nil {
				return fmt.Errorf("%s: %w", s.ID, err)
			}
			out := cmd.OutOrStdout()
			if len(args) == 1 {
				if d.Question != "" {
					fmt.Fprintln(out, d.Question)
				}
				for i, o := range d.Options {
					fmt.Fprintf(out, "  %d. %s\n", i+1, o)
				}
				return nil
			}
			n, err := answer.Match(d, strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			label := d.Options[n-1]
			if err := answer.Send(s.ID, window, n, label, d.Fingerprint); err != nil {
				return fmt.Errorf("%s: %w", s.ID, err)
			}
			fmt.Fprintf(out, "Answered %s: %d. %s\n", s.ID, n, label)
			return nil
		},
	}
	cmd.Flags().IntVarP(&window, "window", "w", 0, "screen window")
	return cmd
}
