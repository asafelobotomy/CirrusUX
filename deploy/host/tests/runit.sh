#!/bin/bash
# Run by run.sh inside `unshare -r`: the gateway's side on a machine without
# systemd (CIRRUS_INIT=runit), against a scratch ROOT. systemctl and
# timedatectl must never be called; tailscaled must never be touched.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
mkdir -p $T/stub $T/root/etc $T/root/usr/share $T/rel/bin
ln -s /usr/share/zoneinfo $T/root/usr/share/zoneinfo
ln -s /usr/share/zoneinfo/UTC $T/root/etc/localtime
ln -s "$H/.." $T/rel/deploy
echo "binary gateway v1" > $T/rel/bin/cirrusux-gateway
for c in systemctl timedatectl; do
	printf '#!/bin/sh\necho "%s $*" >> "$STUBDIR/forbidden"\nexit 1\n' $c > $T/stub/$c
done
# sv: "run:" once linked, and restarts are noted.
cat > $T/stub/sv <<'S'
#!/bin/sh
echo "sv $*" >> "$STUBDIR/calls"
case "$1" in status) [ -L "$2" ] && echo "run: $2: (pid 1) 5s" ;; esac
exit 0
S
printf '#!/bin/sh\necho "{\\"BackendState\\": \\"Running\\"}"\n' > $T/stub/tailscale
printf '#!/bin/sh\necho "useradd $*" >> "$STUBDIR/calls"; touch "$STUBDIR/user"\n' > $T/stub/useradd
printf '#!/bin/sh\n[ "$1" = cirrusux ] && { [ -f "$STUBDIR/user" ]; exit; }\nexec /usr/bin/id "$@"\n' > $T/stub/id
printf '#!/bin/sh\nprintf "install ok installed"\n' > $T/stub/dpkg-query
chmod +x $T/stub/*
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 CONFIG=$T/host.env CIRRUS_RELEASE=$T/rel CIRRUS_INIT=runit
fail() { echo "FAIL: $*"; exit 1; }
E=$ROOT/etc/cirrusux

printf 'HOST_TZ=Europe/London\nCIRRUS_ROLE=server\n' > $CONFIG
$H/cirrusux-setup check cirrusux > $T/out 2>&1 && fail "the server role accepted without systemd"
grep -q "needs systemd" $T/out || { cat $T/out; fail "wrong refusal"; }
$H/cirrusux-setup check sleep > $T/out 2>&1 && fail "the sleep module accepted without systemd"
echo "1 without systemd, only the gateway's side runs"

printf 'HOST_TZ=Europe/London\nCIRRUS_ROLE=gateway\nCIRRUS_SERVER_NAME=atlas\nNET_WOL_IFACE=eth0\n' > $CONFIG
$H/cirrusux-setup apply base cirrusux network > $T/out 2>&1 || { cat $T/out; fail apply; }
[ "$(readlink $ROOT/etc/localtime)" = /usr/share/zoneinfo/Europe/London ] && grep -qx Europe/London $ROOT/etc/timezone || fail "time zone files"
[ "$(stat -c %a $ROOT/etc/sv/cirrusux-gateway/run)" = 755 ] && [ "$(stat -c %a $ROOT/etc/sv/cirrusux-gateway/log/run)" = 755 ] || fail "runit service"
grep -q "cirrusux-gateway -env /etc/cirrusux/gateway.env" $ROOT/etc/sv/cirrusux-gateway/run || fail "run script"
[ -e $ROOT/etc/systemd ] && fail "systemd files written"
[ -L $ROOT/etc/service/cirrusux-gateway ] && fail "gateway started with a placeholder left"
grep -q "stays off until /etc/cirrusux/gateway.env is filled in" $T/out || { cat $T/out; fail "no placeholder warning"; }
grep -q "tailscaled left as it is" $T/out && grep -q "Wake-on-LAN .* skipped without systemd" $T/out || { cat $T/out; fail "network output"; }
grep -q "^CIRRUS_SETUP=split" $E/gateway.env || fail "gateway.env"
echo "2 apply: zone by files, runit service, gateway held back, tailscaled and WoL left alone"

sed -i 's/^CIRRUS_ALLOWED_USERS=.*/CIRRUS_ALLOWED_USERS=me@example.com/; s/^CIRRUS_AGENT_TOKEN=.*/CIRRUS_AGENT_TOKEN=0123456789abcdef0123456789abcdef/' $E/gateway.env
$H/cirrusux-setup apply cirrusux > $T/out 2>&1 || { cat $T/out; fail "apply filled in"; }
[ "$(readlink $ROOT/etc/service/cirrusux-gateway)" = /etc/sv/cirrusux-gateway ] || fail "service not linked into /etc/service"
grep -q "rm /etc/service/cirrusux-gateway" $ROOT/var/lib/cirrusux/setup.log || fail "undo not logged"
$H/cirrusux-setup check base cirrusux network > $T/out 2>&1; rc=$?; [ $rc = 0 ] || { cat $T/out; fail "re-check $rc"; }
echo "3 filled in: linked into /etc/service, undo logged; re-check clean"

echo "binary gateway v2" > $T/rel/bin/cirrusux-gateway
$H/cirrusux-setup apply cirrusux > $T/out 2>&1
grep -q "sv restart $ROOT/etc/service/cirrusux-gateway" $T/calls || fail "no sv restart after a new binary"
[ -f $T/forbidden ] && { cat $T/forbidden; fail "systemctl or timedatectl called"; }
grep -q tailscaled $T/calls && fail "tailscaled touched"
echo "4 new binary: sv restart; systemctl, timedatectl and tailscaled never touched"
