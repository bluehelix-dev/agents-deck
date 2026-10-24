#!/bin/sh
# One SCons for PlatformIO Core and the pioarduino platform, in the PlatformIO
# home of the idf5 projects ($PLATFORMIO_CORE_DIR).
#
# Core runs the build on its own SCons package (tool-scons 4.11). pioarduino
# wants tool-scons 4.8 and, finding another version, deletes the package to
# install its own while Core is already running from it: the build stops with
# "No module named 'SCons.Tool.FortranCommon'". This is the fix from
# docs/flashing.md#troubleshooting, for a clean machine: install the project's
# platform and packages, then put Core's own SCons package in place, its
# package.json labelled with the version pioarduino checks for. Safe to run
# again.
#
#   PLATFORMIO_CORE_DIR=~/.platformio-pioarduino .github/scripts/pioarduino-scons.sh <project dir> <env>
set -eu
project=$1
env=$2
: "${PLATFORMIO_CORE_DIR:?set PLATFORMIO_CORE_DIR to the PlatformIO home of the idf5 projects}"
pkg="$PLATFORMIO_CORE_DIR/packages/tool-scons"

pio pkg install -d "$project" -e "$env"

# The version pioarduino checks for: its platform.json names scons-4.8.1.zip,
# which is tool-scons 4.40801.0 (major, then major minor patch in two digits).
want=$(python3 - "$PLATFORMIO_CORE_DIR" <<'EOF'
import glob, json, os, re, sys
for p in sorted(glob.glob(os.path.join(sys.argv[1], "platforms", "espressif32*", "platform.json"))):
    url = json.load(open(p))["packages"]["tool-scons"]["version"]
    m = re.search(r"scons-(\d+)\.(\d+)\.(\d+)\.zip$", url)
    if m:
        a, b, c = map(int, m.groups())
        print(f"{a}.{a}{b:02d}{c:02d}.0")
        break
EOF
)
[ -n "$want" ] || { echo "pioarduino-scons: no pioarduino platform with a tool-scons in $PLATFORMIO_CORE_DIR" >&2; exit 1; }

# Core's own SCons package, installed by Core itself into a scratch home.
pio_python=$(sed -n '1s/^#! *//p' "$(command -v pio)")  # the Python that runs pio
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
PLATFORMIO_CORE_DIR="$tmp" "$pio_python" -c \
  "from platformio.package.manager.core import get_core_package_dir as g; g('tool-scons')"

rm -rf "$pkg"
cp -R "$tmp/packages/tool-scons" "$pkg"
python3 - "$pkg/package.json" "$want" <<'EOF'
import json, sys
path, want = sys.argv[1:]
d = json.load(open(path))
d["version"] = want
json.dump(d, open(path, "w"), indent=2)
EOF
echo "pioarduino-scons: Core's SCons in $pkg, labelled $want for pioarduino"
