#!/bin/sh
# Regenerates src/proto/link.pb.c and link.pb.h from the Deck Link contract
# (../../proto/agentsdeck/link/v1/link.proto and link.options, relative to the
# kit). Needs nanopb_generator 0.4.9.1, the same version as the vendored
# runtime in src/proto (pb.h says NANOPB_VERSION).
#
#   tools/generate.sh           regenerate in place
#   tools/generate.sh --check   regenerate into a temp dir and fail on any diff
set -eu

kit=$(cd "$(dirname "$0")/.." && pwd)
proto_dir="$kit/../../proto/agentsdeck/link/v1"
out="$kit/src/proto"
want="nanopb-0.4.9.1"

have=$(nanopb_generator --version 2>/dev/null | tail -n 1)
if [ "$have" != "$want" ]; then
	echo "generate.sh: need $want, found '${have:-no nanopb_generator}'" >&2
	exit 1
fi

target="$out"
if [ "${1:-}" = "--check" ]; then
	target=$(mktemp -d)
	trap 'rm -rf "$target"' EXIT
fi

# -T: no timestamp (stable output); -L: include "pb.h" with quotes so the
# runtime next to the generated files is found without extra include paths.
(cd "$proto_dir" && nanopb_generator -q -T -L '#include "%s"' \
	-f link.options -D "$target" link.proto)

# Hat.auto and SetHat.auto are named after a C/C++ keyword; nanopb has no
# rename option, so the struct member becomes auto_ (the wire format is by
# field number and doesn't change). Then say so in the preamble.
for f in "$target/link.pb.h" "$target/link.pb.c"; do
	sed -e 's/^\(    bool \)auto;/\1auto_;/' \
		-e 's/^\(X(a, STATIC, *SINGULAR, BOOL, *\)auto, /\1auto_,/' \
		-e '1s|^/\* Automatically generated nanopb \(.*\) \*/$|/* Automatically generated nanopb \1: do not edit. Regenerate with tools/generate.sh from proto/agentsdeck/link/v1 (link.proto, link.options). */|' \
		"$f" >"$f.tmp"
	mv "$f.tmp" "$f"
done

if [ "${1:-}" = "--check" ]; then
	diff -u "$out/link.pb.h" "$target/link.pb.h"
	diff -u "$out/link.pb.c" "$target/link.pb.c"
	echo "src/proto is up to date"
fi
