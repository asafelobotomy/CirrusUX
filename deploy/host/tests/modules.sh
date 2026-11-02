#!/bin/bash
# Run by run.sh inside `unshare -r`: network, backup and security modules
# against a scratch ROOT, with stand-ins for systemctl, apt and friends.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/root/etc/cirrusux $T/root/sys/class/net/eth0
echo 52:54:00:12:34:56 > $T/root/sys/class/net/eth0/address
cat > $T/stub/systemctl <<'S'
#!/bin/sh
f="$STUBDIR/enabled"; touch "$f"; echo "systemctl $*" >> "$STUBDIR/calls"
case "$1" in
is-enabled|is-active) grep -qx "$3" "$f" ;;
enable) shift; [ "$1" = --now ] && shift; for u; do echo "$u" >> "$f"; done ;;
disable) shift; [ "$1" = --now ] && shift; sed -i "/^$1\$/d" "$f" ;;
*) exit 0 ;;
esac
S
cat > $T/stub/dpkg-query <<'S'
#!/bin/sh
for p; do :; done
grep -qx "$p" "$STUBDIR/installed" 2>/dev/null && printf 'install ok installed'
S
cat > $T/stub/apt-get <<'S'
#!/bin/sh
for p; do case "$p" in -*|install) ;; *) echo "$p" >> "$STUBDIR/installed" ;; esac; done
S
cat > $T/stub/ethtool <<'S'
#!/bin/sh
if [ "$1" = -s ]; then echo "$4" > "$STUBDIR/wol"; exit 0; fi
printf 'Supports Wake-on: pumbg\n\tWake-on: %s\n' "$(cat "$STUBDIR/wol" 2>/dev/null || echo d)"
S
printf '#!/bin/sh\necho "{\\"BackendState\\": \\"NeedsLogin\\"}"\n' > $T/stub/tailscale
printf '#!/bin/sh\necho atlas\n' > $T/stub/hostname
# A stand-in for age: checks it got a recipient, passes the data through.
printf '#!/bin/sh\n[ "$1" = -r ] || exit 9\ncat\n' > $T/stub/age
chmod +x $T/stub/*
touch $T/installed
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 CONFIG=$T/host.env
fail() { echo "FAIL: $*"; exit 1; }
KEY=age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq

# network
printf 'NET_WOL_IFACE=eth0\n' > $CONFIG
$H/cirrusux-setup apply network > $T/out 2>&1 || { cat $T/out; fail "network apply"; }
grep -q "MACAddress=52:54:00:12:34:56" $ROOT/etc/systemd/network/50-cirrusux-wol.link || fail "wol link file"
grep -q "NamePolicy=keep" $ROOT/etc/systemd/network/50-cirrusux-wol.link || fail "naming policy missing: the card could be renamed"
grep -q "switch on Wake-on-LAN for eth0" $T/out && grep -q "isn't logged in" $T/out || { cat $T/out; fail "network output"; }
grep -qx tailscaled.service $T/enabled || fail "tailscaled not enabled"
echo "network: Wake-on-LAN link file keeps the card's name; tailscaled at boot; login reported"

# backup: refuses a bad key, then installs and runs for real
printf 'BACKUP_AGE_RECIPIENT=age-not-a-key\n' > $CONFIG
$H/cirrusux-setup check backup > $T/out 2>&1 && fail "bad key accepted"
mkdir -p $T/data/etc && echo "hello" > $T/data/etc/thing.conf
printf 'BACKUP_AGE_RECIPIENT=%s\nBACKUP_DIR=%s\nBACKUP_PATHS="%s"\nBACKUP_KEEP=2\n' $KEY $T/backups $T/data > $CONFIG
$H/cirrusux-setup apply backup > $T/out 2>&1 || { cat $T/out; fail "backup apply"; }
grep -qx age $T/installed && grep -qx zstd $T/installed || fail "packages"
grep -q "WakeSystem=yes" $ROOT/etc/systemd/system/cirrusux-backup.timer || fail "timer doesn't wake"
grep -qx cirrusux-backup.timer $T/enabled || fail "timer not on"
grep -q "BACKUP_UNIT=cirrusux-backup.service" $ROOT/etc/cirrusux/helper.d/backup.env || fail "helper.d"
[ "$(stat -c %a $ROOT/etc/cirrusux/backup.env)" = 600 ] || fail "backup.env mode"
for i in 1 2 3; do
	CONF=$ROOT/etc/cirrusux/backup.env $ROOT/usr/local/sbin/cirrusux-backup > $T/run.out 2>&1 || { cat $T/run.out; fail "backup run $i"; }
	sleep 1
done
grep -q "^OK .* atlas-system-.*\.tar\.zst\.age$" $T/run.out || { cat $T/run.out; fail "summary line"; }
grep -q "removed old backup" $T/run.out || fail "retention didn't run"
[ "$(ls $T/backups | wc -l)" = 2 ] || { ls $T/backups; fail "BACKUP_KEEP=2 not honoured"; }
ls $T/backups | grep -q partial && fail "a .partial was left"
newest=$(ls $T/backups | sort | tail -1)
zstd -dc $T/backups/$newest | tar -tf - | grep -q "etc/thing.conf" || fail "archive doesn't hold the data"
[ "$(stat -c %a $T/backups/$newest)" = 600 ] || fail "archive mode"
echo "backup: bad key refused; timer wakes the server; runs encrypt, rotate to BACKUP_KEEP, leave no .partial"

# a failing encryption keeps the .partial and fails the run
printf '#!/bin/sh\nexit 3\n' > $T/stub/age
CONF=$ROOT/etc/cirrusux/backup.env $ROOT/usr/local/sbin/cirrusux-backup > $T/run.out 2>&1 && fail "failed encryption reported OK"
grep -q "FAILED" $T/run.out && ls $T/backups | grep -q partial || fail "failure not reported or .partial lost"
echo "backup: a failed encryption fails the run and never looks like a good backup"

# security
printf 'SEC_AV_PATHS="/srv"\n' > $CONFIG
echo clamav-freshclam.service >> $T/enabled
mkdir -p $ROOT/etc/cirrusux && touch $ROOT/etc/cirrusux/firewall.nft
$H/cirrusux-setup apply security > $T/out 2>&1 || { cat $T/out; fail "security apply"; }
grep -q "backend = systemd" $ROOT/etc/fail2ban/jail.d/cirrusux.local || fail "jail"
grep -qx clamav-freshclam.service $T/enabled && fail "freshclam daemon left running"
grep -q "clamscan .*--log=/var/log/cirrusux/av-scan.log /srv$" $ROOT/etc/systemd/system/cirrusux-av-scan.service || fail "scan unit"
grep -q "AV_LOG=/var/log/cirrusux/av-scan.log" $ROOT/etc/cirrusux/helper.d/av.env || fail "av.env"
grep -q "FIREWALL_CHECKS=inet/cirrusux/input:drop" $ROOT/etc/cirrusux/helper.d/security.env && grep -q "FAIL2BAN_JAIL=sshd" $ROOT/etc/cirrusux/helper.d/security.env || fail "security.env"
printf 'NET_WOL_IFACE=eth0\nBACKUP_AGE_RECIPIENT=%s\nBACKUP_DIR=%s\nBACKUP_PATHS="%s"\nBACKUP_KEEP=2\nSEC_AV_PATHS="/srv"\n' $KEY $T/backups $T/data > $CONFIG
$H/cirrusux-setup check network backup security > $T/out 2>&1; rc=$?
[ $rc = 0 ] || { cat $T/out; fail "re-check $rc"; }
echo "security: jail on the journal, scan updates itself (no daemon), panel checks written; re-check clean"
