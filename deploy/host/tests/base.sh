#!/bin/bash
# Run by run.sh inside `unshare -r`, against a scratch ROOT with stand-ins.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/root/etc $T/root/usr/share
ln -s /usr/share/zoneinfo $T/root/usr/share/zoneinfo
ln -s /usr/share/zoneinfo/UTC $T/root/etc/localtime
cat > $T/stub/systemctl <<'S'
#!/bin/sh
f="$STUBDIR/enabled"; [ -f "$f" ] || echo apt-daily-upgrade.timer > "$f"
case "$1" in
is-enabled) grep -qx "$3" "$f" ;;
disable) sed -i "/^$3\$/d" "$f" ;;
*) exit 0 ;;
esac
S
cat > $T/stub/timedatectl <<'S'
#!/bin/sh
ln -sf "/usr/share/zoneinfo/$2" "$ROOT/etc/localtime"
S
printf '#!/bin/sh\necho "useradd $*" >> "$STUBDIR/calls"; touch "$STUBDIR/user"\n' > $T/stub/useradd
printf '#!/bin/sh\n[ "$1" = cirrusux ] && { [ -f "$STUBDIR/user" ]; exit; }\nexec /usr/bin/id "$@"\n' > $T/stub/id
chmod +x $T/stub/*
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 CONFIG=$T/root/etc/cirrusux/host.env
fail() { echo "FAIL: $*"; exit 1; }
printf 'HOST_TZ=Europe/London\n' > $T/host.env; export CONFIG=$T/host.env

$H/cirrusux-setup check base > $T/out; [ $? = 2 ] || fail check
grep -q "create system user cirrusux" $T/out && grep -q "set the time zone to Europe/London" $T/out && grep -q "switch off apt-daily-upgrade.timer" $T/out || { cat $T/out; fail "check output"; }
echo "1 check: user, time zone and automatic updates to change"
$H/cirrusux-setup apply base > /dev/null || fail apply
[ "$(readlink $ROOT/etc/localtime)" = /usr/share/zoneinfo/Europe/London ] || fail "zone"
[ -d $ROOT/etc/cirrusux ] && [ "$(stat -c %a $ROOT/etc/cirrusux)" = 750 ] || fail "conf dir"
$H/cirrusux-setup check base > $T/out; [ $? = 0 ] || { cat $T/out; fail "re-check"; }
echo "2 apply, then re-check is clean"
printf 'HOST_TZ=Mars/Olympus\n' > $CONFIG
$H/cirrusux-setup check base > /dev/null 2>&1 && fail "bad zone accepted"
echo "3 an unknown zone is refused"
