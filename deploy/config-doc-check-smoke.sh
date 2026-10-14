#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT INT TERM

cp "$root/README.md" "$root/README.ko.md" "$root/inc.env" "$tmpdir/"
sed '/MCPROXY_ADMIN_THROTTLE/d' "$tmpdir/README.md" > "$tmpdir/README.md.tmp"
mv "$tmpdir/README.md.tmp" "$tmpdir/README.md"

if "$root/deploy/config-doc-check.sh" "$tmpdir" >/dev/null 2>&1; then
	echo 'configuration documentation check accepted a missing variable' >&2
	exit 1
fi

"$root/deploy/config-doc-check.sh" "$root"
echo 'Configuration documentation negative smoke OK'
