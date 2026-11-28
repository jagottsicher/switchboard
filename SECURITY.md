# Security

## Reporting a vulnerability

Please report vulnerabilities privately. Do not describe them in a public issue.

If the **Security** tab of this repository offers **Report a vulnerability**, use it. Otherwise
open an issue that only asks for a private way to report a security problem, without any
details, and you will be given one.

Include what you found, how to reproduce it, and which version or commit you used. You will get
an answer as soon as possible; fixes go into the next release.

## What switchboard protects, and what it does not

switchboard controls terminals in which coding agents run. Keep these points in mind:

- **Access to a session is access to its user.** Whoever can type into an agent's session, attach
  to it or answer its dialogs can run any command as the user the agent runs as. switchboard
  does not add any isolation on top of that.
- **Remote hosts are reached through your own ssh.** switchboard runs the system `ssh` with the
  host alias from its configuration, so your `~/.ssh/config`, keys and known hosts decide whom you
  connect to. On the remote side, `switchboard serve --stdio` only talks over the standard input
  and output of that ssh session. It does not listen on a network port.
- **Answering dialogs is guarded, not safe.** switchboard picks a choice by its label and sends it
  only if the screen still shows the dialog the choice was made for. Approving a permission
  request from the overview still approves what the agent asked for, so read the request first.
- **Hooks run with the agent's rights.** `switchboard hook` stores status information in the
  state directory of the user (`$XDG_STATE_HOME/switchboard`, mode 0700). It never prints to the
  agent and always exits with status 0.

Sharing sessions with other people, which is on the roadmap, will be organizational separation
and not isolation. Real isolation needs separate users or containers.
