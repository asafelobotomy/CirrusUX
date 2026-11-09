#!/bin/bash
# Run by run.sh inside `unshare -r`: cirrusux-setup init from scripted
# answers, tailscale serve, and pair on a gateway then join on a server,
# against scratch ROOTs with stand-ins for systemctl, id and tailscale.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/rel/bin
ln -s "$H/.." $T/rel/deploy
for b in agent helper dockerfilter gateway idle; do echo "binary $b" > $T/rel/bin/cirrusux-$b; done
cat > $T/stub/systemctl <<'S'
#!/bin/sh
f="$STUBDIR/enabled"; touch "$f"; echo "systemctl $*" >> "$STUBDIR/calls"
case "$1" in
is-enabled|is-active) grep -qx "$3" "$f" ;;
enable) shift; [ "$1" = --now ] && shift; for u; do echo "$u" >> "$f"; done ;;
show) echo Europe/Paris ;;
*) exit 0 ;;
esac
S
# Tailscale: this machine is gw.example.ts.net, logged in as me@example.com.
cat > $T/stub/tailscale <<'S'
#!/bin/sh
echo "tailscale $*" >> "$STUBDIR/calls"
case "$1 $2" in
"status --json") printf '{\n "Self": {\n  "DNSName": "gw.example.ts.net.",\n  "UserID": 1\n },\n "User": {\n  "1": {\n   "LoginName": "me@example.com"\n  }\n }\n}\n' ;;
"serve status") [ -f "$STUBDIR/served" ] && echo "|-- / proxy http://127.0.0.1:8064" ;;
"serve --bg") touch "$STUBDIR/served" ;;
esac
S
printf '#!/bin/sh\necho Europe/Paris\n' > $T/stub/timedatectl
printf '#!/bin/sh\necho "  Supports Wake-on: g"; echo "  Wake-on: g"\n' > $T/stub/ethtool
printf '#!/bin/sh\n[ "$1" = cirrusux ] && exit 0\nexec /usr/bin/id "$@"\n' > $T/stub/id
printf '#!/bin/sh\nprintf "install ok installed"\n' > $T/stub/dpkg-query
chmod +x $T/stub/*
printf 'Iface\tDestination\tGateway\nlo\t0000FEA9\t00000000\nenp3s0\t00000000\t0102A8C0\n' > $T/route
export STUBDIR=$T PATH=$T/stub:$PATH NO_CHOWN=1 CIRRUS_RELEASE=$T/rel PROC_NET_ROUTE=$T/route
fail() { echo "FAIL: $*"; exit 1; }
# use NAME: point setup at the scratch machine NAME.
use() { export ROOT=$T/$1 CONFIG=$T/$1/etc/cirrusux/host.env; mkdir -p $ROOT/etc/cirrusux $ROOT/usr/share/zoneinfo/Europe; touch $ROOT/usr/share/zoneinfo/Europe/Paris; }

# The gateway of a split setup: role 3, the detected login, serve on, a
# server name; then no to applying (it's applied below, a module at a time).
use gw
printf '3\n\n\n\nhomeserver\nn\n' | $H/cirrusux-setup init > $T/out 2>&1 || { cat $T/out; fail "init gateway"; }
for l in 'CIRRUS_ROLE=gateway' 'HOST_TZ=Europe/Paris' 'CIRRUS_ALLOWED_USERS="me@example.com"' 'CIRRUS_SERVER_NAME=homeserver' \
	'CIRRUS_TAILSCALE_SERVE=yes' 'HOST_MODULES="base cirrusux network"'; do
	grep -qxF "$l" $CONFIG || fail "gateway host.env lacks $l: $(cat $CONFIG)"
done
[ "$(stat -c %a $CONFIG)" = 600 ] || fail "host.env mode"
echo "1 init writes the gateway's host.env from the answers and what it detects"

use srv
mkdir -p $ROOT/sys/class/net/enp3s0; echo aa:bb:cc:dd:ee:ff > $ROOT/sys/class/net/enp3s0/address
printf '2\n\nn\nn\n\n\nn\n' | $H/cirrusux-setup init > $T/out 2>&1 || { cat $T/out; fail "init server"; }
for l in 'CIRRUS_ROLE=server' 'NET_WOL_IFACE=enp3s0' 'HOST_MODULES="base cirrusux network sleep"'; do
	grep -qxF "$l" $CONFIG || fail "server host.env lacks $l: $(cat $CONFIG)"
done
grep -q CIRRUS_ALLOWED_USERS $CONFIG && fail "a server has no sign-in"
printf '2\nage1bad\n' | $H/cirrusux-setup init > $T/out 2>&1
grep -q "Replace it" $T/out || fail "an existing host.env should be asked about"
mv $CONFIG $T/srv.env
printf '1\n\n\n\nn\nn\nage1bad\n' | $H/cirrusux-setup init > $T/out 2>&1 && fail "a bad age key accepted"
mv $T/srv.env $CONFIG
echo "2 the server's host.env: wake card from the default route, sleep; bad answers refused"

use gw
$H/cirrusux-setup apply cirrusux > $T/out 2>&1 || { cat $T/out; fail "apply gateway"; }
grep -q "tailscale serve --bg --yes --https=443 http://127.0.0.1:8064" $T/calls || fail "tailscale serve not run"
$H/cirrusux-setup check cirrusux > $T/out 2>&1; rc=$?
[ $rc = 0 ] || fail "check after apply ($rc)"   # the gateway waits for its token, with a warning
grep -q "the panel is on the tailnet" $T/out || fail "serve not seen as done: $(cat $T/out)"
echo "3 CIRRUS_TAILSCALE_SERVE=yes publishes the panel once, then reports it"

$H/cirrusux-setup pair > $T/pair 2>&1 || { cat $T/pair; fail pair; }
code=$(grep -o 'cirrusux1:[A-Za-z0-9+/=]*' $T/pair)
tok=$(sed -n 's/^CIRRUS_AGENT_TOKEN=//p' $ROOT/etc/cirrusux/gateway.env)
[ ${#tok} = 64 ] || fail "no token made"
grep -qx CIRRUS_WAKE=wol $ROOT/etc/cirrusux/gateway.env || fail "wake not set to wol"
grep -qx cirrusux-gateway.service $T/enabled || fail "gateway not started after pair"
grep -q "$tok" $ROOT/var/lib/cirrusux/setup.log && fail "the token was logged"
$H/cirrusux-setup pair > $T/pair2 2>&1
[ "$(grep -o 'cirrusux1:[A-Za-z0-9+/=]*' $T/pair2)" = "$code" ] || fail "a second pair changed the code"
echo "4 pair makes the token, wakes by magic packet, starts the gateway, prints a code"

use srv
$H/cirrusux-setup apply cirrusux > /dev/null 2>&1 || fail "apply server"
echo "not-a-code" | $H/cirrusux-setup join > $T/out 2>&1 && fail "a bad code joined"
echo "$code" | $H/cirrusux-setup join > $T/out 2>&1 || { cat $T/out; fail join; }
A=$ROOT/etc/cirrusux/agent.env
grep -qx "CIRRUS_GATEWAY_URL=https://gw.example.ts.net/agent/snapshot" $A && grep -qx "CIRRUS_AGENT_TOKEN=$tok" $A || fail "agent.env: $(cat $A)"
[ "$(stat -c %a $A)" = 600 ] || fail "agent.env mode changed"
grep -qx "CIRRUS_WAKE_IFACE=enp3s0" $A || fail "agent.env lacks the wake card"
grep -qx cirrusux-agent.service $T/enabled || fail "agent not started after join"
grep -q "$tok" $ROOT/var/lib/cirrusux/setup.log && fail "the token was logged"
grep -q "panel is at https://gw.example.ts.net/" $T/out || fail "join output: $(cat $T/out)"
echo "$code" | $H/cirrusux-setup join > $T/out 2>&1 && grep -q "already reports" $T/out || fail "re-join not a no-op"
echo "5 join fills agent.env from the code, once"
