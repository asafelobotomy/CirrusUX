#!/bin/bash
# Runs the host-module tests: shellcheck, then each test in a private user
# namespace (and network namespace for the firewall), never as real root.
set -eu
cd "$(dirname "$0")/.."
shellcheck -x -s bash cirrusux-setup lib.sh init.sh pair.sh release.sh modules/*.sh files/cirrusux-backup ../../install.sh ../../scripts/bundle.sh
shellcheck ../gateway/cirrusux-router-check ../gateway/runit/run ../gateway/runit/log/run
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
for t in base cirrusux modules runit extras ssh-unit install pairing; do
	echo "$t:"
	unshare -r bash "tests/$t.sh" 2> >(grep -v 'sudo.conf' >&2) || failed "$t" $?
done
if command -v python3 >/dev/null && command -v curl >/dev/null; then
	echo "router check:"
	bash tests/router-check.sh || failed router-check $?
fi
if [ "${CIRRUS_CONTAINER_TESTS:-}" = 1 ] && command -v podman >/dev/null; then
	echo "runit in a container:"
	bash tests/runit-container.sh || failed runit-container $?
fi
if command -v nft >/dev/null; then
	echo "firewall:"
	unshare -rn bash tests/firewall.sh || failed firewall $?
else
	echo "skipping the firewall test: nft is not installed" >&2
fi
