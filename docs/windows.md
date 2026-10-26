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
agentctl web
```

`agentctl install` registers the daemon as a login item (`agents-deck` under
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, run through `conhost --headless` so it has no
window), starts it right away, and replaces a daemon that is already running, so run it again after a
rebuild. The log is `%USERPROFILE%\.local\share\agents-terminal\agentd.log`. To stop it for good:
delete that `Run` value and end `agentctl.exe` in Task Manager.

`agentctl web` opens the dashboard in your default browser with a one-time login link.

## What works

- **Claude Code sessions you run yourself**, in any terminal and in the Claude desktop app: `agentctl
  live` lists them (from `claude agents --json`), `agentctl add <session>` puts one on the deck, where
  its status, activity, context and cost come from its transcript. Background sessions show up too.
- **The browser dashboard** on `127.0.0.1:7340`: the same deck, prompts, stop, remove from deck.
- **Paired devices over Wi-Fi**: the TLS listener on `:7341`, `agentctl pair`, `agentctl devices`, the
  desk panel and boards on Wi-Fi. The deck's CA key is a file under `keys\` in the data folder (there
  is no Keychain); Windows ACLs on your profile folder keep it yours.
- The CLI: `ls`, `sessions`, `resume`, `rm`, `move`, `set`, `get`, `accounts`, `hats`.

## What doesn't, yet

- **`agentctl new`, `attach`, `restore`, `reopen`**: agents the deck starts itself live in tmux, which
  Windows lacks. Start Claude Code the way you always do and add the session instead.
- **Focus** (jump to an agent's terminal) does nothing: it talks to iTerm2. The tile still shows which
  folder the session is in.
- **Boards on USB**: no serial port discovery on Windows yet (`agentctl link ports`). Flash and pair a
  board on a Mac, or use a board that is already on Wi-Fi.
- **Codex** sessions need a `sqlite3.exe` on your PATH (the daemon reads Codex's database with it).
- **Orca, herdr and cmux** are macOS terminals; their watchers find nothing here.

## How it differs under the hood

The Unix-only pieces each have a Windows file beside them (`_windows.go`): process liveness, the
login-shell lookup for `claude`, terminal focus (a no-op), `install` (a `Run` key instead of launchd)
and `attach`. Everything else is the same code: the daemon listens on a unix socket in the data folder
(Windows has had `AF_UNIX` since 2018), the dashboard stays loopback-only, and `go test ./...` passes.
