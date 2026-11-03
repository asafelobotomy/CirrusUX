#!/bin/bash
# Runs the host-module tests: shellcheck, then each test in a private user
# namespace (and network namespace for the firewall), never as real root.
set -eu
cd "$(dirname "$0")/.."
shellcheck -x -s bash cirrusux-setup lib.sh modules/*.sh files/cirrusux-backup
# Private profiles, when this checkout has any.
for f in ../profiles/*/capture.sh; do [ -f "$f" ] && shellcheck -s bash "$f"; done
shellcheck ../gateway/cirrusux-router-check
echo "shellcheck: ok"
if ! unshare -r true 2>/dev/null; then
	echo "skipping module tests: unprivileged user namespaces are off" >&2
	exit 0
fi
# Every failure names its test, even one that stops without saying why.
failed() {
	echo "FAILED: the $1 test (exit $2)" >&2
	exit 1
}
for t in base cirrusux modules; do
	echo "$t:"
	unshare -r bash "tests/$t.sh" 2> >(grep -v 'sudo.conf' >&2) || failed "$t" $?
done
if command -v python3 >/dev/null && command -v curl >/dev/null; then
	echo "router check:"
	bash tests/router-check.sh || failed router-check $?
fi
if command -v nft >/dev/null; then
	echo "firewall:"
	unshare -rn bash tests/firewall.sh || failed firewall $?
else
	echo "skipping the firewall test: nft is not installed" >&2
fi
