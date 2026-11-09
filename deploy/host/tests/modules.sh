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
[ "$1" = update ] && { echo update >> "$STUBDIR/apt-updates"; exit 0; }
for p; do case "$p" in -*|install) ;; *) echo "$p" >> "$STUBDIR/installed" ;; esac; done
S
cat > $T/stub/ethtool <<'S'
#!/bin/sh
if [ "$1" = -s ]; then echo "$4" > "$STUBDIR/wol"; exit 0; fi
printf 'Supports Wake-on: pumbg\n\tWake-on: %s\n' "$(cat "$STUBDIR/wol" 2>/dev/null || echo d)"
S
printf '#!/bin/sh\necho "tailscale $*" >> "$STUBDIR/calls"\necho "{\\"BackendState\\": \\"NeedsLogin\\"}"\n' > $T/stub/tailscale
printf '#!/bin/sh\necho atlas\n' > $T/stub/hostname
printf '#!/bin/sh\necho "sysctl $*" >> "$STUBDIR/calls"\n' > $T/stub/sysctl
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

printf 'NET_TAILSCALE_SET="--accept-dns=false; rm -rf /"\n' > $CONFIG
$H/cirrusux-setup check network > /dev/null 2>&1 && fail "a NET_TAILSCALE_SET that isn't flags accepted"
mkdir -p $ROOT/etc/default && printf 'PORT="41641"\nFLAGS=""\n' > $ROOT/etc/default/tailscaled
printf 'NET_TAILSCALE_SET="--accept-dns=false --accept-routes"\nNET_TAILSCALED_FLAGS="--no-logs-no-support"\n' > $CONFIG
: > $T/calls
$H/cirrusux-setup apply network > $T/out 2>&1 || { cat $T/out; fail "network apply with Tailscale settings"; }
grep -qx "tailscale set --accept-dns=false --accept-routes" $T/calls && [ -f $ROOT/var/lib/cirrusux/tailscale-prefs.before ] || fail "tailscale set, or the preferences before it"
grep -qx 'PORT="41641"' $ROOT/etc/default/tailscaled && grep -qx 'FLAGS="--no-logs-no-support"' $ROOT/etc/default/tailscaled || fail "/etc/default/tailscaled"
grep -q "systemctl restart tailscaled.service" $T/calls || fail "tailscaled not restarted for its flags"
: > $T/calls
$H/cirrusux-setup check network > $T/out 2>&1; [ $? = 0 ] || { cat $T/out; fail "re-check with Tailscale settings"; }
grep -q "tailscale set" $T/calls && fail "re-check ran tailscale set"
echo "network: NET_TAILSCALE_SET applied once, its undo kept; NET_TAILSCALED_FLAGS written and tailscaled restarted"

# backup: refuses a bad key, then installs and runs for real
printf 'BACKUP_AGE_RECIPIENT=age-not-a-key\n' > $CONFIG
$H/cirrusux-setup check backup > $T/out 2>&1 && fail "bad key accepted"
mkdir -p $T/data/etc && echo "hello" > $T/data/etc/thing.conf
printf 'BACKUP_AGE_RECIPIENT=%s\nBACKUP_DIR=%s\nBACKUP_PATHS="%s"\nBACKUP_KEEP=2\n' $KEY $T/backups $T/data > $CONFIG
rm -f $T/apt-updates
$H/cirrusux-setup apply backup > $T/out 2>&1 || { cat $T/out; fail "backup apply"; }
grep -qx age $T/installed && grep -qx zstd $T/installed || fail "packages"
[ "$(wc -l < $T/apt-updates)" = 1 ] || fail "package lists not refreshed once before installing"
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
# stale .partial files are pruned, fresh ones kept
touch -d '5 days ago' $T/backups/atlas-system-old.tar.zst.age.partial; touch $T/backups/atlas-system-new.tar.zst.age.partial
CONF=$ROOT/etc/cirrusux/backup.env $ROOT/usr/local/sbin/cirrusux-backup > $T/run.out 2>&1 || fail "backup run with partials"
[ ! -e $T/backups/atlas-system-old.tar.zst.age.partial ] && [ -e $T/backups/atlas-system-new.tar.zst.age.partial ] || fail "stale .partial pruning"
rm -f $T/backups/*.partial
echo "backup: bad key refused; timer wakes the server; runs encrypt, rotate to BACKUP_KEEP, leave no .partial"

# a failing encryption keeps the .partial and fails the run
printf '#!/bin/sh\nexit 3\n' > $T/stub/age
CONF=$ROOT/etc/cirrusux/backup.env $ROOT/usr/local/sbin/cirrusux-backup > $T/run.out 2>&1 && fail "failed encryption reported OK"
grep -q "FAILED" $T/run.out && ls $T/backups | grep -q partial || fail "failure not reported or .partial lost"
echo "backup: a failed encryption fails the run and never looks like a good backup"


# security
printf 'SEC_AV_PATHS="/srv"\n' > $CONFIG
echo clamav-freshclam.service >> $T/enabled
$H/cirrusux-setup apply security > $T/out 2>&1 || { cat $T/out; fail "security apply"; }
grep -q "backend = systemd" $ROOT/etc/fail2ban/jail.d/cirrusux.local || fail "jail"
grep -qx clamav-freshclam.service $T/enabled && fail "freshclam daemon left running"
grep -q "clamscan .*--log=/var/log/cirrusux/av-scan.log /srv$" $ROOT/etc/systemd/system/cirrusux-av-scan.service || fail "scan unit"
grep -q "AV_LOG=/var/log/cirrusux/av-scan.log" $ROOT/etc/cirrusux/helper.d/av.env || fail "av.env"
grep -q "FAIL2BAN_JAIL=sshd" $ROOT/etc/cirrusux/helper.d/security.env || fail "security.env"
printf 'NET_WOL_IFACE=eth0\nBACKUP_AGE_RECIPIENT=%s\nBACKUP_DIR=%s\nBACKUP_PATHS="%s"\nBACKUP_KEEP=2\nSEC_AV_PATHS="/srv"\n' $KEY $T/backups $T/data > $CONFIG
$H/cirrusux-setup check network backup security > $T/out 2>&1; rc=$?
[ $rc = 0 ] || { cat $T/out; fail "re-check $rc"; }
echo "security: jail on the journal, scan updates itself (no daemon), panel checks written; re-check clean"

# BACKUP_REQUIRE_MOUNT: no disk, no backup, and no folder made on the system disk.
printf '#!/bin/sh\n[ "$1" = -r ] || exit 9\ncat\n' > $T/stub/age
printf '#!/bin/sh\ncat "$STUBDIR/mnt"\n' > $T/stub/findmnt; chmod +x $T/stub/findmnt
printf 'BACKUP_AGE_RECIPIENT=%s\nBACKUP_DIR=/srv/backups\nBACKUP_REQUIRE_MOUNT=/mnt/media\n' $KEY > $CONFIG
$H/cirrusux-setup check backup > $T/out 2>&1 && fail "BACKUP_DIR outside BACKUP_REQUIRE_MOUNT accepted"
grep -q "isn't inside BACKUP_REQUIRE_MOUNT" $T/out || { cat $T/out; fail "wrong refusal"; }
printf 'BACKUP_AGE_RECIPIENT=%s\nBACKUP_DIR=%s/media/BackUp/sys\nBACKUP_PATHS="%s"\nBACKUP_REQUIRE_MOUNT=%s/media\nBACKUP_KEEP=14\n' $KEY $T $T/data $T > $CONFIG
$H/cirrusux-setup apply backup > $T/out 2>&1 || { cat $T/out; fail "backup apply with a mount"; }
grep -qx "RequiresMountsFor=$T/media/BackUp/sys" $ROOT/etc/systemd/system/cirrusux-backup.service || fail "RequiresMountsFor"
mkdir -p $T/media
echo / > $T/mnt
CONF=$ROOT/etc/cirrusux/backup.env $ROOT/usr/local/sbin/cirrusux-backup > $T/run.out 2>&1 && fail "backed up with the disk missing"
grep -q "isn't on $T/media" $T/run.out && [ ! -e $T/media/BackUp ] || { cat $T/run.out; fail "no clear refusal, or the folder was made anyway"; }
echo $T/media > $T/mnt
CONF=$ROOT/etc/cirrusux/backup.env $ROOT/usr/local/sbin/cirrusux-backup > $T/run.out 2>&1 || { cat $T/run.out; fail "backup with the disk there"; }
echo "backup: BACKUP_REQUIRE_MOUNT refuses to write without the disk, and works with it"

# sleep: only for a split setup's server; installs the checker, its timer,
# the sleep hook and the stay-awake commands the panel uses.
mkdir -p $T/rel/bin && echo "idle v1" > $T/rel/bin/cirrusux-idle
printf 'CIRRUS_ROLE=single\nCIRRUS_RELEASE=%s\n' $T/rel > $CONFIG
$H/cirrusux-setup check sleep > $T/out 2>&1 && fail "sleep module accepted the single setup"
grep -q "for a split setup" $T/out || fail "wrong refusal"
printf 'CIRRUS_ROLE=server\nCIRRUS_RELEASE=%s\nSLEEP_IGNORE_WAKE_GRACE_MIN=soon\n' $T/rel > $CONFIG
$H/cirrusux-setup check sleep > /dev/null 2>&1 && fail "a wake grace that isn't a number accepted"
printf 'CIRRUS_ROLE=server\nCIRRUS_RELEASE=%s\nSLEEP_IDLE_MIN=20\nSLEEP_IGNORE_CLIENTS="192.0.2.23"\nSLEEP_IGNORE_CLIENTS_PLAY_MB=5\nSLEEP_IGNORE_WAKE_GRACE_MIN=5\n' $T/rel > $CONFIG
$H/cirrusux-setup apply sleep > $T/out 2>&1 || { cat $T/out; fail "sleep apply"; }
grep -qx "IGNORE_CLIENTS_PLAY_MB=5" $ROOT/etc/cirrusux/sleep.env && grep -qx "IGNORE_WAKE_GRACE_MIN=5" $ROOT/etc/cirrusux/sleep.env || fail "sleep.env: TV settings"
grep -qx "net.netfilter.nf_conntrack_acct=1" $ROOT/etc/sysctl.d/60-cirrusux-conntrack.conf && grep -q "sysctl -q -p" $T/calls || fail "conntrack byte counts"
[ "$(readlink $ROOT/usr/local/sbin/cirrusux-stay-awake)" = cirrusux-idle ] && [ "$(readlink $ROOT/usr/local/sbin/cirrusux-allow-sleep)" = cirrusux-idle ] || fail "stay-awake links"
grep -q "^IDLE_MIN=20" $ROOT/etc/cirrusux/sleep.env && grep -q 'IGNORE_CLIENTS="192.0.2.23"' $ROOT/etc/cirrusux/sleep.env || fail "sleep.env"
[ "$(stat -c %a $ROOT/usr/lib/systemd/system-sleep/cirrusux-idle)" = 755 ] || fail "sleep hook"
grep -qx cirrusux-idle.timer $T/enabled || fail "timer not on"
grep -q "STAY_AWAKE_BIN=/usr/local/sbin/cirrusux-stay-awake" $ROOT/etc/cirrusux/helper.d/sleep.env || fail "helper.d"
grep -qx conntrack $T/installed || fail "conntrack not installed"
$H/cirrusux-setup check sleep > $T/out 2>&1; [ $? = 0 ] || { cat $T/out; fail "sleep re-check"; }
echo "sleep: refuses the single setup; checker, timer, hook, stay-awake links, TV settings, byte counts and panel hookup; re-check clean"

# apps: Podman by default, the filter pointed at Podman, folders and user.
printf 'APPS_MEDIA_ROOTS="/srv/media /mnt/disk"\n' > $CONFIG
cat > $T/stub/id <<'S'
#!/bin/sh
case "$1 $2" in
"cirrusux-apps "*) [ -f "$STUBDIR/appsuser" ]; exit ;;
"-u cirrusux-apps") cut -d: -f1 "$STUBDIR/appsuser"; exit ;;
"-g cirrusux-apps") cut -d: -f2 "$STUBDIR/appsuser"; exit ;;
"cirrusux "*) exit 0 ;;
esac
exec /usr/bin/id "$@"
S
# useradd notes the IDs it was given (999 if none).
cat > $T/stub/useradd <<'S'
#!/bin/sh
u=999 g=999
while [ $# -gt 1 ]; do case "$1" in -u) u=$2; shift ;; -g) g=$2; shift ;; esac; shift; done
echo "$u:$g" > "$STUBDIR/appsuser"
S
cat > $T/stub/getent <<'S'
#!/bin/sh
grep -E "^$2:|^[^:]*:x:$2:" "$STUBDIR/groups" 2>/dev/null
S
printf '#!/bin/sh\necho "$3:x:$2:" >> "$STUBDIR/groups"\n' > $T/stub/groupadd
chmod +x $T/stub/getent $T/stub/groupadd
printf '#!/bin/sh\nexec /usr/bin/mkdir "$@"\n' > $T/stub/mkdir
chmod +x $T/stub/id $T/stub/useradd $T/stub/mkdir
$H/cirrusux-setup apply apps > $T/out 2>&1 || { cat $T/out; fail "apps apply"; }
grep -qx podman $T/installed && grep -qx nftables $T/installed && grep -qx podman.socket $T/enabled || fail "podman"
grep -q "upstream /run/podman/podman.sock" $ROOT/etc/systemd/system/cirrusux-dockerfilter.service.d/podman.conf || fail "filter drop-in"
grep -qx "ReadWritePaths=" $ROOT/etc/systemd/system/cirrusux-dockerfilter.service.d/podman.conf || fail "drop-in keeps Docker's socket path"
grep -q "systemctl restart cirrusux-dockerfilter.socket" $T/calls || fail "filter not restarted after the drop-in"
grep -q "CONTAINER_RUNTIME=podman" $ROOT/etc/cirrusux/helper.d/apps.env && grep -q "MEDIA_ROOTS=/srv/media /mnt/disk" $ROOT/etc/cirrusux/helper.d/apps.env || fail "apps.env"
[ -d $ROOT/srv/cirrusux/apps ] && [ -d $ROOT/var/lib/cirrusux/apps ] || fail "folders"
grep -q "media folder /srv/media doesn't exist yet" $T/out || fail "no warning for a missing media folder"
$H/cirrusux-setup check apps > $T/out 2>&1; [ $? = 0 ] || { cat $T/out; fail "apps re-check"; }
printf 'APPS_MEDIA_ROOTS="/srv/My Media"\n' > $CONFIG
$H/cirrusux-setup check apps > /dev/null 2>&1 && fail "a path with a space accepted"
# Values the helper would refuse must be refused here, or the panel loses every module.
for bad in 'APPS_DIR=/srv/apps/' 'APPS_DIR=/srv//apps' 'APPS_DIR=/srv/../apps' 'APPS_BIND=1.2.3' 'APPS_BIND=300.1.1.1'; do
	printf '%s\n' "$bad" > $CONFIG
	$H/cirrusux-setup check apps > /dev/null 2>&1 && fail "$bad accepted"
done
printf 'APPS_BIND=192.0.2.10\n' > $CONFIG
$H/cirrusux-setup check apps > $T/out 2>&1; [ $? = 1 ] && { cat $T/out; fail "a good APPS_BIND refused"; }
echo "apps: Podman with its socket, the filter pointed at it, folders, user and panel hookup; re-check clean"

# APPS_MEDIA_UID/GID: a fresh user gets the media's IDs and a media group;
# an existing one with other IDs is only warned about.
rm -f $T/appsuser
printf 'APPS_MEDIA_UID=1000\n' > $CONFIG
$H/cirrusux-setup check apps > /dev/null 2>&1 && fail "APPS_MEDIA_UID without APPS_MEDIA_GID accepted"
printf 'APPS_MEDIA_UID=1000\nAPPS_MEDIA_GID=1001\n' > $CONFIG
$H/cirrusux-setup apply apps > $T/out 2>&1 || { cat $T/out; fail "apps apply with media IDs"; }
grep -qx "media:x:1001:" $T/groups && [ "$(cat $T/appsuser)" = 1000:1001 ] || fail "media group or user IDs"
$H/cirrusux-setup check apps > $T/out 2>&1; [ $? = 0 ] || { cat $T/out; fail "re-check with media IDs"; }
echo 999:999 > $T/appsuser
$H/cirrusux-setup apply apps > $T/out 2>&1 || fail "apply with a user of other IDs"
grep -q "cirrusux-apps is 999:999, not 1000:1001" $T/out && [ "$(cat $T/appsuser)" = 999:999 ] || { cat $T/out; fail "existing user changed, or no warning"; }
echo "apps: media IDs make the user and the media group; an existing user is only warned about"
