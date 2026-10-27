# agents deck on Windows

The daemon and the browser dashboard run on Windows 10 and 11 (amd64 and arm64). There is no tray
app yet (the menu bar app is macOS only) and no release archive: you build `agentctl` from a clone.

## Install

Needs Go 1.24+ and Claude Code (`claude` on your PATH, or in `%USERPROFILE%\.local\bin`, where its
installer puts it).

```powershell
git clone https://github.com/brushknight/agents-deck; cd agents-deck\backend
go build -o "$env:USERPROFILE\.local\bin\agentctl.exe" ./cmd/agentctl
agentctl install
agentctl hooks install
agentctl web
```

`agentctl install` registers the daemon as a login item (`agents-deck` under
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, run through `conhost --headless` so it has no
window), starts it right away, and replaces a daemon that is already running, so run it again after a
rebuild. The log is `%USERPROFILE%\.local\share\agents-terminal\agentd.log`. To stop it for good:
delete that `Run` value and end `agentctl.exe` in Task Manager.

`agentctl hooks install` puts the deck's hook into Claude Code's user settings
(`%USERPROFILE%\.claude\settings.json`, next to any hooks of your own), so every session you start
from then on, in a terminal or in the Claude desktop app, tells the deck what it runs and what it
waits for; without it the deck reads the sessions' transcripts, which is enough for status but not
for permission prompts. The hook is a short `agentctl hook` call that exits at once when no daemon
runs. (Claude Code runs hooks through Git Bash on Windows, which eats the backslashes of a Windows
path: the installed command uses forward slashes for that reason.)

`agentctl web` opens the dashboard in your default browser with a one-time login link.

Two settings in `%USERPROFILE%\.local\share\agents-terminal\config.json` are worth knowing:
`"liveAutoAdd": true` puts every session the daemon finds on the deck by itself (otherwise you add
them with `agentctl add` or a tap on a free tile), and the `"hats"` map is where picked hats live.

## What works

- **Claude Code sessions you run yourself**, in any terminal and in the Claude desktop app: `agentctl
  live` lists them (from `claude agents --json`), `agentctl add <session>` puts one on the deck, where
  its status, activity, context and cost come from its transcript, and a question it stops at shows
  on its card with its choices (answered in the session itself). With `agentctl hooks install` the
  sessions report through hooks instead, and permission prompts show too
  ([features.md](features.md#claude-sessions-you-start-yourself)). Background sessions show up
  too.
  `"liveAutoAdd": true` in `config.json` puts every found session on the deck by itself
  ([features.md](features.md#claude-sessions-you-start-yourself)).
- **The browser dashboard** on `127.0.0.1:7340`: the same deck, prompts, stop, remove from deck.
- **Paired devices over Wi-Fi**: the TLS listener on `:7341`, `agentctl pair`, `agentctl devices`, the
  desk panel, boards on Wi-Fi and the [Android app](phone.md). Pair those with `--host <address>`:
  a phone can't resolve a Windows machine name, and that is what the code or bundle names
  otherwise. Use the PC's LAN address at home, or its VPN address (Tailscale, WireGuard) when the
  phone should reach the deck from anywhere; a device paired with the VPN address works in both
  places. Removing sessions from the deck on a phone needs `agentctl devices scope <name> admin`.
  The deck's CA key is a file under `keys\` in the data folder (there is no Keychain); Windows ACLs
  on your profile folder keep it yours.
- The CLI: `ls`, `live`, `add`, `hooks`, `sessions`, `resume`, `rm`, `move`, `set`, `get`, `accounts`,
  `hats`, `devices`, `pair`.
- **Workflows** (the Workflow tool, `/workflows`) show on their session's tile as a bar of phases,
  read from the run's journal; nothing to set up.

## What doesn't, yet

- **`agentctl new`, `attach`, `restore`, `reopen`**: agents the deck starts itself live in tmux, which
  Windows lacks. Start Claude Code the way you always do and add the session instead.
- **Focus** (jump to an agent's terminal) and **answering from the deck**: both type into the
  session's terminal, through iTerm2 or tmux, which Windows has no equivalent for, and the Claude
  desktop app has no terminal to type into at all. The daemon reports `"focus": false` in its state
  and the cards leave the focus buttons out; a watched session's prompt shows with its choices, and
  you answer it in the session itself. On Windows the deck is a second pair of eyes, not a remote
  control.
- **Boards on USB**: no serial port discovery on Windows yet (`agentctl link ports`). Flash and pair a
  board on a Mac, or use a board that is already on Wi-Fi.
- **Codex** sessions need a `sqlite3.exe` on your PATH (the daemon reads Codex's database with it).
- **Orca, herdr and cmux** are macOS terminals; their watchers find nothing here.

## How it differs under the hood

The Unix-only pieces each have a Windows file beside them (`_windows.go`): process liveness, the
login-shell lookup for `claude`, terminal focus (reported as unavailable), `install` (a `Run` key
instead of launchd) and `attach`. Everything else is the same code: the daemon listens on a unix socket in the data folder
(Windows has had `AF_UNIX` since 2018), the dashboard stays loopback-only, and `go test ./...` passes.
