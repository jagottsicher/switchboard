package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

func newListCmd() *cobra.Command {
	var asJSON, agentsOnly bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List screen sessions with the status of their agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := state.Open("")
			if err != nil {
				return err
			}
			infos, warn, err := sessions.Collect(store, sessions.Options{Prune: true, Detect: true, Self: os.Getenv("STY")})
			if err != nil {
				return err
			}
			if warn != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning:", warn)
			}
			if agentsOnly {
				filtered := infos[:0]
				for _, s := range infos {
					if s.IsAgent() {
						filtered = append(filtered, s)
					}
				}
				infos = filtered
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(infos)
			}
			if len(infos) == 0 {
				fmt.Fprintln(out, "No screen sessions found.")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SESSION\tSCREEN\tSTATUS\tAGE\tMESSAGE")
			for _, s := range infos {
				screenState := string(s.State)
				if s.Multi {
					screenState += "/multi"
				}
				status, since := "-", "-"
				if s.Status != "" {
					status = string(s.Status)
				}
				if s.Agent != nil {
					since = sessions.Age(s.Agent.UpdatedAt)
				}
				msg := sessions.Shorten(s.Message, 60)
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, screenState, status, since, msg)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVarP(&agentsOnly, "agents", "a", false, "show only sessions with agents")
	return cmd
}

// findWithWindow resolves query to a session and picks the window: the --window flag if given,
// otherwise the window an agent hook reported, otherwise window 0.
func findWithWindow(cmd *cobra.Command, query string, flagWindow int) (screen.Session, int, error) {
	s, err := screen.Find(query)
	if err != nil {
		return s, 0, err
	}
	if cmd.Flags().Changed("window") {
		return s, flagWindow, nil
	}
	if store, err := state.Open(""); err == nil {
		if rec, ok, _ := store.Get(s.ID); ok && rec.Window != "" {
			if w, err := strconv.Atoi(rec.Window); err == nil {
				return s, w, nil
			}
		}
	}
	return s, flagWindow, nil
}

func newPeekCmd() *cobra.Command {
	var lines, window int
	cmd := &cobra.Command{
		Use:   "peek SESSION",
		Short: "Show the last screen lines of a session without attaching",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, window, err := findWithWindow(cmd, args[0], window)
			if err != nil {
				return err
			}
			content, err := screen.Hardcopy(s.ID, window)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, l := range screen.Tail(content, lines) {
				fmt.Fprintln(out, l)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 40, "number of lines (0 = all, including scrollback)")
	cmd.Flags().IntVarP(&window, "window", "w", 0, "screen window")
	return cmd
}

func newSendCmd() *cobra.Command {
	var noEnter bool
	var window int
	cmd := &cobra.Command{
		Use:   "send SESSION [TEXT...]",
		Short: "Type text into a session and submit it with Enter",
		Long: "Types TEXT into the session and then presses Enter. Multiple words are joined\n" +
			"with spaces. With TEXT \"-\", the text is read from standard input.\n" +
			"Without TEXT, only Enter is sent.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.Join(args[1:], " ")
			if len(args) == 2 && args[1] == "-" {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				text = strings.TrimRight(string(data), "\n")
			}
			if text == "" && noEnter {
				return errors.New("nothing to send: no text and --no-enter")
			}
			s, window, err := findWithWindow(cmd, args[0], window)
			if err != nil {
				return err
			}
			return screen.Stuff(s.ID, window, text, !noEnter)
		},
	}
	cmd.Flags().BoolVar(&noEnter, "no-enter", false, "do not press Enter after the text")
	cmd.Flags().IntVarP(&window, "window", "w", 0, "screen window")
	return cmd
}

// execAttach replaces the running process with screen. Swappable for tests.
var execAttach = func(args, env []string) error {
	path, err := exec.LookPath(args[0])
	if err != nil {
		return screen.ErrNotInstalled
	}
	return syscall.Exec(path, args, env)
}

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach SESSION",
		Short: "Attach to a session (screen -x, detach again with Ctrl-a d)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := screen.Find(args[0])
			if err != nil {
				return err
			}
			if screen.InsideScreen() {
				fmt.Fprintln(cmd.ErrOrStderr(), "Note: switchboard itself runs inside screen. "+
					"Ctrl-a goes to the outer screen; reach the inner session with Ctrl-a a.")
			}
			return execAttach(screen.AttachArgs(s.ID), screen.AttachEnv())
		},
	}
}

func newNewCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "new NAME [-C DIR] -- COMMAND [ARGS...]",
		Short: "Start a command in a new, detached screen session",
		Example: "  switchboard new api -C ~/src/api -- agent --resume\n" +
			"  switchboard new build -- make watch",
		Args: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash != 1 || len(args) < 2 {
				return errors.New("usage: switchboard new NAME [-C DIR] -- COMMAND [ARGS...]")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir != "" {
				info, err := os.Stat(dir)
				if err != nil {
					return err
				}
				if !info.IsDir() {
					return fmt.Errorf("%s is not a directory", dir)
				}
			}
			s, err := screen.New(args[0], dir, args[1:])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), s.ID)
			// Mark the session as an agent. Update merges with a record that the agent's first
			// hook may already have written.
			store, err := state.Open("")
			if err == nil {
				_, err = store.Update(s.ID, func(r *state.Record) {
					r.Agent = true
					if r.Window == "" {
						r.Window = "0"
					}
					if r.Cwd == "" {
						r.Cwd = dir
					}
				})
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: could not mark the session as an agent:", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&dir, "directory", "C", "", "working directory of the command")
	return cmd
}
