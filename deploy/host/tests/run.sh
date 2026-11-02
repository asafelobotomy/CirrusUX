#!/bin/bash
# Runs the host-module tests: shellcheck, then each test in a private user
# namespace (and network namespace for the firewall), never as real root.
set -eu
cd "$(dirname "$0")/.."
shellcheck -x -s bash cirrusux-setup lib.sh modules/*.sh files/cirrusux-backup ../profiles/*/capture.sh
echo "shellcheck: ok"
if ! unshare -r true 2>/dev/null; then
	echo "skipping module tests: unprivileged user namespaces are off" >&2
	exit 0
fi
for t in base cirrusux modules; do
	echo "$t:"
	unshare -r bash "tests/$t.sh" 2> >(grep -v 'sudo.conf' >&2)
done
if command -v nft >/dev/null; then
	echo "firewall:"
	unshare -rn bash tests/firewall.sh
else
	echo "skipping the firewall test: nft is not installed" >&2
fi
