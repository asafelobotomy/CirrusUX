#!/bin/bash
# Run by run.sh inside `unshare -r`: the ssh module picks the right SSH
# service (ssh.service on Debian, sshd.service elsewhere) to reload, and
# refuses when neither exists. systemctl is a stand-in that lists only the
# units named in $STUBDIR/units.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/root/etc/cirrusux
cat > $T/stub/systemctl <<'S'
#!/bin/sh
echo "systemctl $*" >> "$STUBDIR/calls"
case "$1" in
list-unit-files) shift; for u; do grep -qx "$u" "$STUBDIR/units" && echo "$u enabled enabled"; done; exit 0 ;;
*) exit 0 ;;
esac
S
printf '#!/bin/sh\nexit 0\n' > $T/stub/sshd
printf '#!/bin/sh\nexit 0\n' > $T/stub/systemd-run
chmod +x $T/stub/*
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 CONFIG=$T/host.env NO_SESSION_SCAN=1 CIRRUS_INIT=systemd
unset SSH_CONNECTION NO_SYSTEMD
fail() { echo "FAIL: $*"; exit 1; }
D=$ROOT/etc/ssh/sshd_config.d/50-restrict-sources.conf
printf 'SSH_ALLOW="alice@192.0.2.0/24"\nSSH_CONFIRM_SECONDS=60\n' > $CONFIG

try() { # UNITS... : apply from scratch with these units installed
	rm -f $D $T/calls; rm -rf $ROOT/var/lib/cirrusux/ssh; : > $T/units
	for u; do echo "$u" >> $T/units; done
	$H/cirrusux-setup apply ssh > $T/out 2>&1
}

try ssh.service || { cat $T/out; fail "apply with ssh.service"; }
grep -qx "systemctl reload ssh.service" $T/calls || { cat $T/calls; fail "ssh.service not reloaded"; }
grep -q "reload sshd.service" $T/calls && fail "sshd.service reloaded though absent"
echo "1 ssh unit: Debian's ssh.service is the one reloaded"

try sshd.service || { cat $T/out; fail "apply with sshd.service"; }
grep -qx "systemctl reload sshd.service" $T/calls || { cat $T/calls; fail "sshd.service not reloaded"; }
grep -q "reload ssh.service" $T/calls && fail "ssh.service reloaded though absent"
echo "2 ssh unit: sshd.service is the one reloaded when that is the name"

try ssh.service sshd.service || { cat $T/out; fail "apply with both"; }
grep -qx "systemctl reload ssh.service" $T/calls || fail "ssh.service should win when both exist"
grep -q "reload sshd.service" $T/calls && fail "both units reloaded"
echo "3 ssh unit: ssh.service wins when both exist"

try && fail "applied with neither unit"
grep -q "neither ssh.service nor sshd.service" $T/out || { cat $T/out; fail "wrong refusal"; }
[ ! -e $D ] && ! grep -q " reload " $T/calls || fail "something changed with no SSH unit"
echo "4 ssh unit: neither exists, so nothing is changed"
