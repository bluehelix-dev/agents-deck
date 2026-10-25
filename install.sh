#!/bin/sh
# agents deck installer: downloads a release from GitHub, checks it against
# the release's SHA256SUMS, installs agentctl to ~/.local/bin and starts the
# daemon (launchd on macOS, a systemd user service on Linux). Add --app for the
# menu bar app (~/Applications, macOS only). In a clone, --source builds from code.
#
# Linux note: no prebuilt releases are published, so Linux always builds from
# source (--source is implied when run from a clone). The menu bar app is
# macOS-only; on Linux use the web dashboard (agentctl web).
#
#   curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --app            # also the menu bar app
#   curl -fsSL …/install.sh | sh -s -- --version v0.2.0
#   ./install.sh --source --app                         # from a clone (needs Go; Flutter for --app)
#   --no-daemon: install agentctl but don't (re)start its login item
set -eu

repo="brushknight/agents-deck"
version=""
app=0
daemon=1
source=0
while [ $# -gt 0 ]; do
  case "$1" in
    --app) app=1 ;;
    --no-daemon) daemon=0 ;;
    --source) source=1 ;;
    --version) version="$2"; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

os="$(uname -s)"; arch="$(uname -m)"
case "$os/$arch" in
  Darwin/arm64)          target="darwin_arm64" ;;
  Linux/x86_64)          target="linux_amd64" ;;
  Linux/aarch64|Linux/arm64) target="linux_arm64" ;;
  *) echo "agents deck has no build for $os/$arch (macOS arm64, or Linux x86_64/arm64)" >&2; exit 1 ;;
esac

# No prebuilt releases are published for Linux, so build from source there.
# Auto-switch when run from a clone; otherwise point the way.
if [ "$os" = Linux ] && [ "$source" != 1 ]; then
  if [ -f "$(cd "$(dirname "$0")" 2>/dev/null && pwd)/backend/go.mod" ]; then
    echo "Linux: no prebuilt release, building from source."
    source=1
  else
    echo "Linux has no prebuilt release. Clone and build from source:" >&2
    echo "  git clone https://github.com/$repo && cd agents-deck && ./install.sh --source" >&2
    exit 1
  fi
fi

# What the deck runs agents with: worth a word, not a stop.
command -v tmux >/dev/null 2>&1 || echo "note: agentctl runs agents in tmux; install it with: brew install tmux"
command -v claude >/dev/null 2>&1 || echo "note: no claude on PATH; install Claude Code to start agents with agentctl new"

finish() {
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) echo "add ~/.local/bin to your PATH, e.g.: echo 'export PATH=\$HOME/.local/bin:\$PATH' >> ~/.zshrc" ;;
  esac
  echo "done · try: agentctl new"
}

# Register the daemon to run at login. macOS uses launchd (agentctl install);
# Linux uses a systemd user service (the launchd path is macOS-only).
setup_daemon() {
  bin="$HOME/.local/bin/agentctl"
  if [ "$os" = Darwin ]; then
    "$bin" install
    return
  fi
  # Linux: systemd user service, best effort.
  if command -v systemctl >/dev/null 2>&1; then
    unit_dir="$HOME/.config/systemd/user"
    mkdir -p "$unit_dir"
    cat > "$unit_dir/agents-deck.service" <<EOF
[Unit]
Description=agents deck daemon
After=default.target

[Service]
ExecStart=$bin serve
Restart=on-failure
Environment=PATH=$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin

[Install]
WantedBy=default.target
EOF
    systemctl --user daemon-reload 2>/dev/null || true
    if systemctl --user enable --now agents-deck.service 2>/dev/null; then
      echo "  daemon → systemd user service 'agents-deck' (running)"
    else
      echo "  systemd user unit written; start it with:"
      echo "    systemctl --user enable --now agents-deck.service"
      echo "  (or just run it in a terminal: agentctl serve)"
    fi
  else
    echo "  no systemd found — start the daemon yourself: agentctl serve"
  fi
}

# The menu bar app is macOS-only (Flutter macOS target). On Linux the web
# dashboard (agentctl web) is the cross-platform equivalent.
app_note_linux() {
  echo "note: the menu bar app is macOS-only; on Linux use the web dashboard: agentctl web"
}

if [ "$source" = 1 ]; then
  root="$(cd "$(dirname "$0")" && pwd)"
  [ -f "$root/backend/go.mod" ] || { echo "--source runs from a clone of the repo: ./install.sh --source" >&2; exit 1; }
  command -v go >/dev/null 2>&1 || { echo "--source needs Go 1.24+: brew install go" >&2; exit 1; }
  v="$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)"
  echo "agents deck from source ($v)"
  mkdir -p "$HOME/.local/bin"
  (cd "$root/backend" && go build -ldflags "-X main.version=$v" -o "$HOME/.local/bin/agentctl" ./cmd/agentctl)
  echo "  agentctl → ~/.local/bin/agentctl"
  if [ "$daemon" = 1 ]; then setup_daemon; fi
  if [ "$app" = 1 ]; then
    if [ "$os" != Darwin ]; then
      app_note_linux
    else
      command -v flutter >/dev/null 2>&1 || { echo "--app needs Flutter 3.44+: brew install --cask flutter" >&2; exit 1; }
      (cd "$root/clients/macos" && flutter pub get >/dev/null && flutter build macos --release)
      mkdir -p "$HOME/Applications"
      rm -rf "$HOME/Applications/agents deck.app"
      ditto "$root/clients/macos/build/macos/Build/Products/Release/agents deck.app" "$HOME/Applications/agents deck.app"
      echo "  menu bar app → ~/Applications/agents deck.app"
      open "$HOME/Applications/agents deck.app"
    fi
  fi
  finish
  exit 0
fi

if [ -z "$version" ] && [ -z "${AGENTS_DECK_BASE_URL:-}" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$version" ] || { echo "no release found: install from a clone instead (git clone https://github.com/$repo && cd agents-deck && ./install.sh --source)" >&2; exit 1; }
fi
v="${version#v}"
# AGENTS_DECK_BASE_URL points at another copy of the release (testing).
base="${AGENTS_DECK_BASE_URL:-https://github.com/$repo/releases/download/$version}"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

fetch() { curl -fsSL -o "$tmp/$1" "$base/$1"; }
verify() { # the file's line from SHA256SUMS must match
  (cd "$tmp" && grep "  $1\$" SHA256SUMS | shasum -a 256 -c -) >/dev/null ||
    { echo "checksum mismatch for $1: not installing" >&2; exit 1; }
}

echo "agents deck $version"
fetch SHA256SUMS
cli="agentctl_${v}_${target}.tar.gz"
fetch "$cli"; verify "$cli"
mkdir -p "$HOME/.local/bin"
tar -xzf "$tmp/$cli" -C "$tmp"
install -m 0755 "$tmp/agentctl" "$HOME/.local/bin/agentctl"
echo "  agentctl → ~/.local/bin/agentctl"
if [ "$daemon" = 1 ]; then setup_daemon; fi

if [ "$app" = 1 ]; then
  zip="agents-deck_${v}_macos_arm64.zip"
  fetch "$zip"; verify "$zip"
  mkdir -p "$HOME/Applications"
  rm -rf "$HOME/Applications/agents deck.app"
  ditto -x -k "$tmp/$zip" "$HOME/Applications"
  echo "  menu bar app → ~/Applications/agents deck.app"
  open "$HOME/Applications/agents deck.app"
fi

finish
