# switchboard

A control room for coding agents running in GNU screen sessions.

If you run several coding agents, each in its own `screen` session, you usually have to detach
from one and attach to the next just to find out whether an agent is still working or waiting
for your input. switchboard gives you one overview instead: it lists your sessions, shows what is
on their screens without attaching, types input into them, and attaches with `screen -x` when you
need full interaction.

switchboard builds on GNU screen. It is not a terminal multiplexer of its own, and it does not
need a daemon.

## Status

Early development. The interactive overview and the commands below work, including agent status
from hooks and from the screen contents. Multi-host support is not implemented yet (see
[Roadmap](#roadmap)).

## Requirements

- Linux or another Unix-like system
- GNU screen (tested with 4.09)
- Go 1.24 or newer to build from source

## Installation

```sh
go install github.com/jagottsicher/switchboard/cmd/switchboard@develop
```

The binary ends up in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure that directory is in
your `PATH`.

### Short name `swb`

`swb` is the short name for switchboard. It is the same program: every command works the same
way under both names, and the help shows the name you used. `go install` only creates
`switchboard`, so add the short name yourself, either as a link next to the binary or as a shell
alias:

```sh
ln -s "$(go env GOPATH)/bin/switchboard" "$(go env GOPATH)/bin/swb"   # works in scripts and over ssh
alias swb=switchboard                                                 # interactive shells only
```

The link follows the binary, so rebuilding or reinstalling switchboard updates `swb` as well.
Packages, once there are any, will install `swb` as a link.

## Usage

### Interactive overview

Run `switchboard` (or `swb`) without a command to open the overview. The upper part lists all
screen sessions, with agents that wait for input at the top. The lower part shows the screen of
the selected session and follows it live. The overview refreshes every two seconds.

| Key | Action |
|---|---|
| `↑`/`↓`, `k`/`j`, `g`/`G` | Select a session |
| `enter` | Attach with `screen -x`; detach with `Ctrl-a d` to come back to the overview |
| `s` | Type a line and send it to the selected session with Enter; an empty line sends only Enter |
| `/` | Filter by session name, status, directory or message; `esc` clears the filter |
| `a` | Show only agents, or all sessions |
| `r` | Refresh now |
| `q` | Quit |

If switchboard itself runs inside screen, the session it runs in is marked `(here)` and cannot be
attached. From an attached session you come back with `Ctrl-a a d`, because the first `Ctrl-a`
goes to the outer screen. Over ssh, start the overview with `ssh -t host switchboard`; without a
terminal it exits with an error. A command given to ssh runs without your interactive shell setup,
so aliases do not apply there and the binary has to be in the `PATH` of a non-interactive shell,
or you call it with its full path.

### Commands

```sh
switchboard list [-a] [--json]                 # sessions with agent status; -a: agents only
switchboard peek SESSION [-n 40] [-w WINDOW]   # show the last lines without attaching
switchboard send SESSION "text" [--no-enter]   # type text and press Enter; "-" reads stdin
switchboard attach SESSION                     # screen -x; detach again with Ctrl-a d
switchboard new NAME [-C DIR] -- COMMAND...    # start an agent in a new detached session
switchboard notify [--status S] "text"         # report status/message from inside a session
switchboard hooks print|install|uninstall      # manage the agent hooks
```

`SESSION` is the full screen ID (`12345.name`), the PID, or a unique session name. `peek` and
`send` use the window an agent reported, unless `-w` is given.

`list` sorts sessions that need attention first. The status of an agent is one of:

| Status | Meaning |
|---|---|
| `waiting` | The agent needs input or a permission decision. |
| `working` | The agent is busy. |
| `idle` | The agent has finished its turn. |
| `exited` | The agent session has ended. |
| `unknown` | The session is marked as an agent, but nothing has been reported yet. |

## Agent status

switchboard learns the status of an agent from hooks that the agent runs on its events. Each
hook calls `switchboard hook`, which stores the event for the screen session it runs in (taken
from `$STY` and `$WINDOW`). The hook never prints anything, always exits with status 0 and does
not call screen, so it cannot block or slow down the agent. Problems are logged to `hook.log` in
the state directory (`$XDG_STATE_HOME/switchboard`, by default `~/.local/state/switchboard`).

Hooks are currently provided for Claude Code. To set them up, run this on every machine where
your agents run:

```sh
switchboard hooks install
```

It shows what it is going to change, asks for confirmation, writes a backup and merges the hooks
into the user settings (`~/.claude/settings.json`, or `$CLAUDE_CONFIG_DIR/settings.json`).
Existing hooks and settings stay as they are; running it again replaces older switchboard entries
instead of adding duplicates. Use `switchboard hooks print` to see the settings without changing
anything, and `switchboard hooks uninstall` to remove them. Running agents usually pick up the
change on their own; otherwise restart them.

Other agents and scripts can report their status with `switchboard notify`:

```sh
switchboard notify --status waiting "Need a decision on the schema"
switchboard notify --status idle "Build finished"
```

The typical setup is to log in to the machine that runs your agents via ssh and start
switchboard there.

### Status from the screen

`list` and the overview also read the visible screen of every session and recognize the
screens of Claude Code: working (the footer offers to interrupt with Esc), waiting (a
permission, question or trust dialog) and idle (the input line). For a waiting agent, the
question of the dialog becomes the message. This covers what hooks cannot report: after an
interrupt with Esc or a denied permission, the agent sends no further event, but the screen
shows the input line again. It also finds agents in sessions that have no hooks and were not
started with `switchboard new`. While one of the agent's menus is open, such as `/config` or
`/model`, the status is unknown, because these menus can be opened while the agent works.

A hook event less than five seconds old takes precedence over the screen, because the screen
lags behind; after that, a recognized screen wins. If the screen is not recognized, the status
from hooks and `notify` stays. The recognition only uses plain text in the bottom lines and was
tested with Claude Code 2.1; other versions may need adjustments.

## Known limitations

- `peek` relies on `screen -X hardcopy`. screen 4.09 stores only the low byte of each character
  in a hardcopy, so umlauts and other Latin-1 characters come out right, but box-drawing
  characters and other symbols outside Latin-1 are shown garbled.
- `send` escapes the characters screen interprets (`\`, `^`, `$`) and splits long text into
  chunks, because screen silently drops `stuff` arguments longer than about 760 bytes.
- The overview shows screens through the same hardcopy, with the same limits.
- The status from the screen only covers Claude Code so far. Other agents report through
  `switchboard notify`.
- Without hooks, switchboard does not know which window of a session runs the agent and reads
  window 0.
- The choices of a dialog depend on the dialog: "No" is option 3 when creating a file but
  option 4 for a shell command. The trust dialog has no numbers and preselects "No, exit", so a
  bare Enter quits the agent. A digit picks its choice at once, without confirmation. To answer
  a dialog by hand, check its choices with `peek` first and use `send --no-enter`.

## Roadmap

1. Answering agent dialogs from the overview.
2. Several hosts in one overview via ssh.
3. Controlled access for other developers to shared sessions.

## Security

Access to an agent's terminal means running code as that agent's user. Anyone who can send input
to a session can do whatever that user can do.

## License

[GNU Affero General Public License v3.0](LICENSE)
