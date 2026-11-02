#!/bin/bash
# Run by run.sh inside `unshare -r`: root in a private user namespace,
# with stand-ins for systemctl and id, against a scratch ROOT.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/root/etc/cirrusux $T/rel/bin
ln -s "$H/.." $T/rel/deploy
for b in agent helper dockerfilter gateway; do echo "binary $b v1" > $T/rel/bin/cirrusux-$b; done
cat > $T/stub/systemctl <<'S'
#!/bin/sh
f="$STUBDIR/enabled"; touch "$f"; echo "systemctl $*" >> "$STUBDIR/calls"
case "$1" in
is-enabled) grep -qx "$3" "$f" ;;
is-active) grep -qx "$3" "$f" ;;
enable) shift; [ "$1" = --now ] && shift; for u; do echo "$u" >> "$f"; done ;;
*) exit 0 ;;
esac
S
printf '#!/bin/sh\n[ "$1" = cirrusux ] && exit 0\nexec /usr/bin/id "$@"\n' > $T/stub/id
chmod +x $T/stub/*
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 CONFIG=$T/root/etc/cirrusux/host.env CIRRUS_RELEASE=$T/rel
printf 'CIRRUS_ROLE=single\nCIRRUS_SERVER_NAME=atlas\n' > $CONFIG
fail() { echo "FAIL: $*"; exit 1; }
E=$ROOT/etc/cirrusux

$H/cirrusux-setup check cirrusux > $T/out; [ $? = 2 ] || fail "check"
[ ! -e $E/gateway.env ] || fail "check wrote a file"
echo "1 check reports, writes nothing"

$H/cirrusux-setup apply cirrusux > $T/out 2>&1 || { cat $T/out; fail apply; }
[ "$(stat -c %a $ROOT/etc/sudoers.d/cirrusux)" = 440 ] || fail "sudoers mode"
[ "$(readlink $ROOT/usr/local/sbin/cirrusux-setup)" = "$T/rel/deploy/host/cirrusux-setup" ] || fail "cirrusux-setup not linked onto the PATH"
t1=$(grep ^CIRRUS_AGENT_TOKEN= $E/gateway.env | cut -d= -f2); t2=$(grep ^CIRRUS_AGENT_TOKEN= $E/agent.env | cut -d= -f2)
[ ${#t1} = 64 ] && [ "$t1" = "$t2" ] || fail "token not shared ($t1 / $t2)"
grep -q "^CIRRUS_SETUP=single" $E/gateway.env && grep -q "^CIRRUS_SERVER_NAME=atlas" $E/gateway.env || fail "gateway.env values"
grep -qx cirrusux-agent.service $T/enabled || fail "agent not started"
grep -qx cirrusux-gateway.service $T/enabled && fail "gateway started with a placeholder left"
grep -q "stays off until /etc/cirrusux/gateway.env is filled in: CIRRUS_ALLOWED_USERS" $T/out || fail "no placeholder warning"
echo "2 apply installs, shares a token, starts the agent, holds the gateway back"

sed -i 's/^CIRRUS_ALLOWED_USERS=.*/CIRRUS_ALLOWED_USERS=me@example.com/' $E/gateway.env
$H/cirrusux-setup apply cirrusux > /dev/null 2>&1
grep -qx cirrusux-gateway.service $T/enabled || fail "gateway not started once filled in"
$H/cirrusux-setup check cirrusux > $T/out 2>&1; rc=$?; [ $rc = 0 ] || { cat $T/out; fail "re-check $rc"; }
grep -q "me@example.com" $E/gateway.env || fail "config overwritten"
echo "3 filled in: gateway starts; re-check is clean; config left alone"

echo "binary agent v2" > $T/rel/bin/cirrusux-agent
: > $T/calls
$H/cirrusux-setup apply cirrusux > $T/out 2>&1
grep -q "restart cirrusux-agent.service" $T/calls || fail "no restart after new binary"
ls $ROOT/usr/local/bin/ | grep -q "cirrusux-agent.bak-" || fail "old binary not kept"
echo "4 new binary: old one kept, agent restarted"

printf 'CIRRUS_ROLE=single\nCIRRUS_ACTIONS=no\n' > $CONFIG
$H/cirrusux-setup apply cirrusux > /dev/null 2>&1
tail -1 $ROOT/etc/sudoers.d/cirrusux | grep -q "NOPASSWD: CIRRUS_READ$" || fail "read-only sudoers"
echo "5 CIRRUS_ACTIONS=no: read-only sudoers rule"
