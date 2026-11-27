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
from hooks. Status detection from the screen contents and multi-host support are not implemented
yet (see [Roadmap](#roadmap)).

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
terminal it exits with an error.

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

## Known limitations

- `peek` relies on `screen -X hardcopy`. screen 4.09 stores only the low byte of each character
  in a hardcopy, so umlauts and other Latin-1 characters come out right, but box-drawing
  characters and other symbols outside Latin-1 are shown garbled.
- `send` escapes the characters screen interprets (`\`, `^`, `$`) and splits long text into
  chunks, because screen silently drops `stuff` arguments longer than about 760 bytes.
- The overview shows screens through the same hardcopy, with the same limits.
- When you interrupt an agent with Esc or deny a permission, the agent sends no final event, so
  its status may stay at `working` or `waiting`. Detecting this from the screen contents is
  planned.

## Roadmap

1. A fallback that derives the agent status from the screen contents.
2. Several hosts in one overview via ssh.
3. Controlled access for other developers to shared sessions.

## Security

Access to an agent's terminal means running code as that agent's user. Anyone who can send input
to a session can do whatever that user can do.

## License

[GNU Affero General Public License v3.0](LICENSE)
