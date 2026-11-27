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

Early development. The commands below work; agent status detection, the TUI and multi-host
support are not implemented yet (see [Roadmap](#roadmap)).

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

## Usage

```sh
switchboard list [--json]                      # list all screen sessions
switchboard peek SESSION [-n 40] [-w WINDOW]   # show the last lines without attaching
switchboard send SESSION "text" [--no-enter]   # type text and press Enter; "-" reads stdin
switchboard attach SESSION                     # screen -x; detach again with Ctrl-a d
switchboard new NAME [-C DIR] -- COMMAND...    # start a command in a new detached session
```

`SESSION` is the full screen ID (`12345.name`), the PID, or a unique session name.

The typical setup is to log in to the machine that runs your agents via ssh and start
switchboard there.

## Known limitations

- `peek` relies on `screen -X hardcopy`. screen 4.09 stores only the low byte of each character
  in a hardcopy, so umlauts and other Latin-1 characters come out right, but box-drawing
  characters and other symbols outside Latin-1 are shown garbled.
- `send` escapes the characters screen interprets (`\`, `^`, `$`) and splits long text into
  chunks, because screen silently drops `stuff` arguments longer than about 760 bytes.

## Roadmap

1. Agent status (working, waiting for input, idle, exited) from agent hooks, with a fallback
   that analyzes the screen contents.
2. A terminal UI with a live overview, peek, send and attach.
3. Several hosts in one overview via ssh.
4. Controlled access for other developers to shared sessions.

## Security

Access to an agent's terminal means running code as that agent's user. Anyone who can send input
to a session can do whatever that user can do.

## License

[GNU Affero General Public License v3.0](LICENSE)
