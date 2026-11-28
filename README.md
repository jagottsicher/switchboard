# switchboard

*Here we go!*

A control room for coding agents running in GNU screen sessions.

If you run several coding agents, each in its own `screen` session, you usually have to detach
from one and attach to the next just to find out whether an agent is still working or waiting
for your input. switchboard gives you one overview instead. It lists your sessions with the status
of their agents and shows what is on their screens without attaching. It types input into them,
answers their dialogs, and attaches with `screen -x` when you need full interaction.

switchboard builds on GNU screen. It is not a terminal multiplexer of its own, it does not need a
daemon, and it works with the screen sessions you already have.

```
switchboard · 4 sessions · 1 waiting
  SESSION           STATUS   AGE   SCREEN    MESSAGE / DIRECTORY
› 12345.api         waiting  3s    detached  Do you want to proceed?
  23456.web         working  40s   detached  ~/src/web
  agents:3456.docs  idle     4m    attached  ~/src/docs
  45678.notes       -        -     detached
── 12345.api · window 0 ──────────────────────────────────────────
 Bash command
 go test ./...
 Do you want to proceed?
   1. Yes
   2. Yes, and always allow access to ~/src/api from this project
   3. No

1-3 answer  enter attach  s send  / filter  a agents  r refresh  q quit  (back: Ctrl-a d)
```

The list shows all sessions, with the agent that waits for you first. `agents:3456.docs` runs on
another machine. Below the list, switchboard shows the screen of the selected session.

## Contents

- [What you can do](#what-you-can-do)
- [Working with screen](#working-with-screen)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Interactive overview](#interactive-overview)
- [Commands](#commands)
- [Agent status](#agent-status)
- [Several hosts](#several-hosts)
- [Known limitations](#known-limitations)
- [Roadmap](#roadmap)
- [Security](#security)
- [License](#license)

## What you can do

- **See every session at a glance.** The overview lists all screen sessions and marks the ones
  that run an agent as `waiting`, `working` or `idle`. Agents that wait for you come first.
- **Look without attaching.** The screen of the selected session is shown live below the list.
  `peek` does the same on the command line.
- **Type into a session.** Send a line of text, or just Enter, without attaching.
- **Answer dialogs.** When an agent asks for permission or offers choices, pick one from the
  overview. switchboard asks you to confirm the label and sends nothing if the dialog has changed
  in the meantime.
- **Attach when you need to.** `enter` attaches with `screen -x`; `Ctrl-a d` brings you back to
  the overview.
- **Start agents.** `switchboard new` starts a command in a new detached session and marks it as
  an agent.
- **Watch several machines.** Sessions on other hosts that you reach with ssh appear in the same
  overview and can be used the same way.
- **Report from scripts.** Any script or agent can report a status and a message with
  `switchboard notify`.

## Working with screen

switchboard assumes one way of working, and fits best if you follow it:

1. **One agent per screen session.** Each agent runs in its own session, usually one per project
   or Git clone. Name the session after the project, because you address sessions by name:

   ```sh
   cd ~/src/api
   screen -S api            # start the session, then start the agent in it
   ```

   or, in one step, with the session marked as an agent from the start:

   ```sh
   switchboard new api -C ~/src/api -- your-agent --resume
   ```

2. **Detach, never quit.** Leave a session with `Ctrl-a d`. The agent keeps running, also when
   your ssh connection drops, and you can come back to it at any time.
3. **Work from the machine that runs the agents.** Log in with `ssh -t host` and start
   `switchboard` (or `swb`) there. To keep the overview itself running across disconnects, start
   it inside a screen session of its own.
4. **Attach only for real work.** Use the overview to see who needs you, answer simple dialogs
   and send short replies. Attach with `enter` when you want to talk to an agent at length, and
   detach with `Ctrl-a d` to return to the overview.

### Why screen and not tmux

switchboard is deliberately built for GNU screen only, and does not support tmux:

- screen is preinstalled or one package away on almost every Unix system, including old and
  minimal servers where agents often run.
- It works with your existing sessions. There is nothing to configure in screen, no plugin and
  no special session type: sessions you started by hand show up just like the ones started with
  `switchboard new`.
- screen can be controlled from outside: `screen -X stuff` types into a session and
  `screen -X hardcopy` reads its screen. switchboard needs nothing else.
- `screen -x` shares a session instead of taking it over. switchboard always attaches this way,
  so a session that is attached in another terminal, for example on your laptop, stays attached
  there, and both see the same screen.

### Tips

- **Windows.** A session can have several windows. Agents that report through hooks also report
  their window, and switchboard reads and types into that window. Without hooks it uses
  window 0, so start the agent in the first window or use `-w` with `peek`, `send` and `answer`.
- **Nested screen.** If switchboard runs inside screen, its own session is marked `(here)` and
  cannot be attached. From a session attached out of it, you come back with `Ctrl-a a d`,
  because the first `Ctrl-a` goes to the outer screen.
- **Scrollback.** `peek -n 0` shows the whole scrollback of a session. How much screen keeps is
  set with `defscrollback` in `~/.screenrc`, for example `defscrollback 10000`.
- **Several viewers.** You can attach to the same session from several terminals at once, from
  switchboard or with `screen -x` directly. Everybody types into the same agent.

## Requirements

- Linux or another Unix-like system
- GNU screen (tested with 4.09)
- Go 1.24 or newer to build from source
- For other hosts: ssh access without a password prompt, for example with a key in your ssh agent

## Installation

```sh
go install github.com/jagottsicher/switchboard/cmd/switchboard@develop
```

The binary ends up in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure that directory is in
your `PATH`. To install it somewhere else, such as `~/.local/bin`, set `GOBIN`:

```sh
GOBIN=~/.local/bin go install github.com/jagottsicher/switchboard/cmd/switchboard@develop
```

Run the same command again to update.

Until the first release, the default branch has no Go module yet, and the Go checksum database
cannot verify it. If `go install` fails with `verifying go.mod … 404 Not Found`, tell Go to fetch
switchboard directly from GitHub and to skip that check:

```sh
GOPRIVATE=github.com/jagottsicher/switchboard go install github.com/jagottsicher/switchboard/cmd/switchboard@develop
```

### Short name `swb`

`swb` is the short name for switchboard. It is the same program: every command works the same
way under both names, and the help shows the name you used. `go install` only creates
`switchboard`, so add the short name yourself, either as a link next to the binary or as a shell
alias:

```sh
ln -s switchboard "$(dirname "$(command -v switchboard)")/swb"  # works in scripts and over ssh
alias swb=switchboard                                        # interactive shells only
```

The link follows the binary, so updating switchboard updates `swb` as well. Packages, once there
are any, will install `swb` as a link.

## Quick start

On the machine that runs your agents:

```sh
switchboard hooks install                                # agents report their status
switchboard new api -C ~/src/api -- your-agent --resume  # or use your existing sessions
switchboard                                              # open the overview
```

Hooks are currently provided for Claude Code; see [Agent status](#agent-status). Without hooks,
switchboard still recognizes Claude Code from its screen, and other agents can report through
`switchboard notify`.

## Interactive overview

Run `switchboard` (or `swb`) without a command to open the overview. The upper part lists all
screen sessions of this machine and of the [configured hosts](#several-hosts), with agents that
wait for input at the top. The lower part shows the screen of the selected session and follows it
live. The list takes at most two fifths of the terminal and scrolls when there are more
sessions; the rest stays free for the screen. The overview refreshes every two seconds.

| Key | Action |
|---|---|
| `↑`/`↓`, `k`/`j`, `g`/`G` | Select a session |
| `enter` | Attach with `screen -x`; detach with `Ctrl-a d` to come back to the overview |
| `s` | Type a line and send it to the selected session with Enter; an empty line sends only Enter |
| `1`–`9` | Answer the dialog of a waiting agent with that choice, after confirming its label with `y` |
| `/` | Filter by session name, status, directory or message; `esc` clears the filter |
| `a` | Show only agents, or all sessions |
| `r` | Refresh now |
| `q` | Quit |

The key help shows `1-N answer` when the selected session waits in a dialog whose choices could
be read. After a digit, switchboard reads the dialog again and asks, for example,
`Answer 12345.api with y (other key cancels): 4 "No"`. Only `y` sends the choice. Right before
sending, the screen is read once more, and if it no longer shows the same dialog, nothing is
sent.

Over ssh, start the overview with `ssh -t host switchboard`; without a terminal it exits with an
error. A command given to ssh runs without your interactive shell setup, so aliases do not apply
there and the binary has to be in the `PATH` of a non-interactive shell, or you call it with its
full path.

## Commands

```sh
switchboard list [-a] [--json] [--all|--host H] # sessions with agent status; -a: agents only
switchboard peek SESSION [-n 40] [-w WINDOW]    # show the last lines without attaching
switchboard send SESSION "text" [--no-enter]    # type text and press Enter; "-" reads stdin
switchboard answer SESSION [CHOICE]             # pick a dialog choice by its label, e.g. "No"
switchboard attach SESSION                      # screen -x; detach again with Ctrl-a d
switchboard new NAME [-C DIR] -- COMMAND...     # start an agent in a new detached session
switchboard notify [--status S] "text"          # report status and message from a session
switchboard hooks print|install|uninstall       # manage the agent hooks
switchboard hosts check                         # check the configured hosts
```

`SESSION` is the full screen ID (`12345.name`), the PID, or a unique session name. For a session
on a configured host, put the host name in front: `HOST:SESSION`. `peek`, `send` and `answer` use
the window an agent reported, unless `-w` is given.

`answer` without a choice prints the question and the choices of the dialog. With a choice, it
picks the choice with exactly that label, regardless of case, and sends its digit without Enter.
Numbers are not accepted: the agent acts on a digit at once, and the numbers differ between
dialogs. A label that is not offered sends nothing. The choice goes to the dialog that is open at
that moment, which may be a newer one than you listed; the output names its question.

`list` sorts sessions that need attention first. The status of an agent is one of:

| Status | Meaning |
|---|---|
| `waiting` | The agent needs input or a permission decision. |
| `working` | The agent is busy. |
| `idle` | The agent has finished its turn. |
| `exited` | The agent session has ended. |
| `unknown` | The session is marked as an agent, but nothing has been reported yet, or one of its menus is open. |

## Agent status

switchboard combines three sources: hooks that the agent runs on its events, messages sent with
`switchboard notify`, and the visible screen of each session.

### Hooks

Each hook calls `switchboard hook`, which stores the event for the screen session it runs in
(taken from `$STY` and `$WINDOW`). The hook never prints anything, always exits with status 0 and
does not call screen, so it cannot block or slow down the agent. Problems are logged to
`hook.log` in the state directory (`$XDG_STATE_HOME/switchboard`, by default
`~/.local/state/switchboard`).

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

### notify

Other agents and scripts report their status with `switchboard notify`, run inside their screen
session:

```sh
switchboard notify --status waiting "Need a decision on the schema"
switchboard notify --status idle "Build finished"
```

### Status from the screen

`list` and the overview also read the visible screen of every session and recognize the screens
of Claude Code: working (the footer offers to interrupt with Esc), waiting (a permission,
question or trust dialog) and idle (the input line). For a waiting agent, the question of the
dialog becomes the message.

This covers what hooks cannot report: after an interrupt with Esc or a denied permission, the
agent sends no further event, but the screen shows the input line again. It also finds agents in
sessions that have no hooks and were not started with `switchboard new`. While one of the
agent's menus is open, such as `/config` or `/model`, the status is unknown, because these menus
can be opened while the agent works.

A hook event less than five seconds old takes precedence over the screen, because the screen lags
behind; after that, a recognized screen wins. If the screen is not recognized, the status from
hooks and `notify` stays. The recognition only uses plain text in the bottom lines and was tested
with Claude Code 2.1; other versions may need adjustments.

## Several hosts

switchboard can show and control the sessions of other machines that you reach with ssh. List
them in `~/.config/switchboard/config.toml` (or `$XDG_CONFIG_HOME/switchboard/config.toml`):

```toml
[[host]]
name = "agents"                     # name in switchboard, as in agents:SESSION
ssh = "agents.example.org"          # host alias for ssh; defaults to name
binary = "~/.local/bin/switchboard" # switchboard on that machine; defaults to switchboard
```

On every host:

1. Install the same version of switchboard as on your machine. Both ends check that they speak
   the same protocol and tell you if they do not.
2. Set `binary` to its path, unless `switchboard` is in the `PATH` of a non-interactive shell
   there. A command run over ssh does not read your interactive shell setup, so `~/.local/bin`
   or `~/go/bin` are often missing from it.
3. Run `switchboard hooks install` there, so the agents on that host report their status.

Then check the setup with `switchboard hosts check`. The overview shows the sessions of all hosts
together; `list --all` does the same on the command line, and `list --host NAME` shows one host.

switchboard runs the system `ssh` with only the host alias, so your `~/.ssh/config` applies with
its user, port, keys and jump hosts. One ssh connection per host stays open while the overview
runs; switchboard starts `switchboard serve --stdio` there and exchanges JSON over it. It does
not listen on a network port. Attaching to a remote session runs `ssh -t HOST screen -x SESSION`
and shares that connection, which ssh keeps open for ten minutes after the last use. A host that
cannot be reached is marked offline and tried again after 30 seconds; the other hosts are not
held up by it.

## Known limitations

- `peek` and the overview rely on `screen -X hardcopy`. screen 4.09 stores only the low byte of
  each character in a hardcopy, so umlauts and other Latin-1 characters come out right, but
  box-drawing characters and other symbols outside Latin-1 are shown garbled.
- `send` escapes the characters screen interprets (`\`, `^`, `$`) and splits long text into
  chunks, because screen silently drops `stuff` arguments longer than about 760 bytes.
- The status from the screen only covers Claude Code so far. Other agents report through
  `switchboard notify`.
- Without hooks, switchboard does not know which window of a session runs the agent and reads
  window 0.
- The choices of a dialog depend on the dialog: "No" is option 3 when creating a file but option
  4 for a shell command. This is why `answer` and the overview go by the label.
- The trust dialog has no numbers and preselects "No, exit", so a bare Enter quits the agent. It
  cannot be answered from switchboard; attach to answer it. The same applies when a very narrow
  window wraps the choices of a dialog so that they cannot be read, and to choices beyond 9.
- In a narrow window, a wrapped question whose continuation starts with a capital letter, such as
  a file name, is shown only from that line on. The choices are not affected.
- The age of a status is computed with the clock of the machine that shows it, so clocks that
  differ between hosts shift the ages of remote sessions.

## Roadmap

1. More ways to talk to agents from the overview, such as free-text answers to questions and
   notifications when an agent starts waiting.
2. Controlled access for other developers to shared sessions.

## Security

Access to an agent's terminal means running code as that agent's user. Anyone who can send input
to a session can do whatever that user can do. See [SECURITY.md](SECURITY.md) for details and for
how to report a vulnerability.

## License

[GNU Affero General Public License v3.0](LICENSE)
