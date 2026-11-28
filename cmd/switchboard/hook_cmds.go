package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jagottsicher/switchboard/internal/hook"
	"github.com/jagottsicher/switchboard/internal/state"
)

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hook",
		Short: "Handle an agent hook event (called by the agent, reads JSON on stdin)",
		Long: "Handles one agent hook event. The agent calls this command; it reads the event as\n" +
			"JSON on stdin and stores it for the screen session in $STY. It never prints anything\n" +
			"and always exits with status 0. Problems are logged to hook.log in the state directory.",
		DisableFlagParsing: true,
		SilenceErrors:      true,
		Run: func(cmd *cobra.Command, _ []string) {
			hook.Run(os.Stdin, os.Getenv)
		},
	}
}

func newNotifyCmd() *cobra.Command {
	var statusFlag, session, window string
	cmd := &cobra.Command{
		Use:   "notify [--status STATUS] [TEXT...]",
		Short: "Report a message and status for the current screen session",
		Long: "Stores a message, and optionally a status, for the screen session the command runs\n" +
			"in ($STY and $WINDOW). Agents and scripts without hook support can use it to report\n" +
			"progress. STATUS is one of working, waiting, idle, exited, unknown.",
		Example: "  switchboard notify --status waiting \"Need a decision on the schema\"\n" +
			"  switchboard notify --status idle \"Build finished\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.Join(args, " ")
			var st state.Status
			if statusFlag != "" {
				var err error
				if st, err = state.ParseStatus(statusFlag); err != nil {
					return err
				}
			}
			if text == "" && st == "" {
				return errors.New("nothing to report: give a text, --status or both")
			}
			if session == "" {
				session = os.Getenv("STY")
				if window == "" {
					window = os.Getenv("WINDOW")
				}
			}
			if session == "" {
				return errors.New("not running inside screen ($STY is not set); use --session")
			}
			store, err := state.Open("")
			if err != nil {
				return err
			}
			_, err = hook.Notify(store, session, window, st, text)
			return err
		},
	}
	cmd.Flags().StringVar(&statusFlag, "status", "", "new status of the agent")
	cmd.Flags().StringVar(&session, "session", "", "screen session ID (default: $STY)")
	cmd.Flags().StringVar(&window, "window", "", "screen window (default: $WINDOW)")
	return cmd
}

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Show or install the agent hooks that report status to switchboard",
	}
	cmd.AddCommand(newHooksPrintCmd(), newHooksInstallCmd(false), newHooksInstallCmd(true))
	return cmd
}

// uniquePath returns p, or p with a numeric suffix if p already exists.
func uniquePath(p string) string {
	candidate := p
	for i := 2; ; i++ {
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", p, i)
	}
}

func hookCommand(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return hook.Command()
}

func newHooksPrintCmd() *cobra.Command {
	var command string
	cmd := &cobra.Command{
		Use:   "print",
		Short: "Print the hooks settings to merge into the agent's settings.json",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := hookCommand(command)
			if err != nil {
				return err
			}
			out, err := hook.Snippet(c)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	cmd.Flags().StringVar(&command, "command", "", "hook command to use (default: absolute path of switchboard + \" hook\")")
	return cmd
}

func newHooksInstallCmd(remove bool) *cobra.Command {
	var command, settings string
	var yes bool
	use, short := "install", "Add the hooks to the agent's settings.json (asks first, makes a backup)"
	if remove {
		use, short = "uninstall", "Remove the switchboard hooks from the agent's settings.json"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := settings
			if path == "" {
				var err error
				if path, err = hook.SettingsPath(); err != nil {
					return err
				}
			}
			var c string
			if !remove {
				var err error
				if c, err = hookCommand(command); err != nil {
					return err
				}
			}
			old, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			out, changed, err := hook.Merge(old, c, remove)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			w := cmd.OutOrStdout()
			if !changed {
				fmt.Fprintf(w, "%s is already up to date.\n", path)
				return nil
			}
			if remove {
				fmt.Fprintf(w, "This removes all switchboard hooks from %s.\n", path)
			} else {
				fmt.Fprintf(w, "This adds hooks for %d events to %s:\n", len(hook.Events), path)
				names := make([]string, len(hook.Events))
				for i, ev := range hook.Events {
					names[i] = ev.Name
				}
				fmt.Fprintf(w, "  %s\n  command: %s\n", strings.Join(names, ", "), c)
				fmt.Fprintln(w, "Existing hooks and other settings stay unchanged; older switchboard entries are replaced.")
			}
			backup := ""
			if old != nil {
				backup = uniquePath(path + ".bak-" + time.Now().Format("20060102-150405"))
				fmt.Fprintf(w, "A backup goes to %s.\n", backup)
			}
			if !yes {
				fmt.Fprint(w, "Continue? [y/N] ")
				answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				answer = strings.ToLower(strings.TrimSpace(answer))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(w, "Nothing changed.")
					return nil
				}
			}
			if err := hook.WriteSettings(path, out, backup); err != nil {
				return err
			}
			fmt.Fprintf(w, "Done. Running agents usually pick up the change by themselves; otherwise restart them.\n")
			return nil
		},
	}
	if !remove {
		cmd.Flags().StringVar(&command, "command", "", "hook command to use (default: absolute path of switchboard + \" hook\")")
	}
	cmd.Flags().StringVar(&settings, "settings", "", "settings file (default: the agent's user settings.json)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
