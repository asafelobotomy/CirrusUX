#!/bin/bash
# The gateway on a machine without systemd, for real: a Debian container
# whose PID 1 is tini, running runsvdir. cirrusux-setup installs the gateway
# role there; runit must start it as cirrusux, answering on 127.0.0.1:8064.
# Needs podman and the network (Debian packages); run.sh runs it when
# CIRRUS_CONTAINER_TESTS=1.
set -eu
H=$(cd "$(dirname "$0")/../../.." && pwd)
T=$(mktemp -d)
NAME=cirrusux-runit-test-$$
trap 'podman rm -f $NAME >/dev/null 2>&1; rm -rf "$T"' EXIT
fail() { echo "FAIL: $*"; podman logs $NAME 2>&1 | tail -30; exit 1; }

mkdir -p $T/bin
(cd "$H" && CGO_ENABLED=0 go build -trimpath -o $T/bin/cirrusux-gateway ./cmd/gateway)

cat > $T/inside.sh <<'SH'
#!/bin/sh
set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq && apt-get install -y -qq --no-install-recommends tini runit curl ca-certificates tzdata procps >/dev/null
# A root-owned copy: root runs setup's code.
mkdir -p /opt/cirrusux && cp -r /src/deploy /src/bin /opt/cirrusux/
printf 'HOST_MODULES="base cirrusux"\nHOST_TZ=Europe/London\nCIRRUS_ROLE=gateway\nCIRRUS_SERVER_NAME=homeserver\nCIRRUS_ALLOWED_USERS=me@example.com\nCIRRUS_RELEASE=/opt/cirrusux\n' > /tmp/host.env
CONFIG=/tmp/host.env /opt/cirrusux/deploy/host/cirrusux-setup apply > /tmp/setup1.out 2>&1
# The token is the one value setup can't know on the gateway's side.
sed -i "s/^CIRRUS_AGENT_TOKEN=.*/CIRRUS_AGENT_TOKEN=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')/" /etc/cirrusux/gateway.env
echo 'CIRRUS_TAILSCALE_PEER=-' >> /etc/cirrusux/gateway.env
CONFIG=/tmp/host.env /opt/cirrusux/deploy/host/cirrusux-setup apply > /tmp/setup2.out 2>&1
touch /tmp/ready
exec runsvdir -P /etc/service
SH
chmod 755 $T/inside.sh

podman run -d --name $NAME -v "$H:/src:ro" -v "$T/bin:/src/bin:ro" -v "$T/inside.sh:/inside.sh:ro" \
	--entrypoint /bin/sh docker.io/library/debian:trixie-slim -c 'apt-get update -qq >/dev/null && apt-get install -y -qq tini >/dev/null && exec tini -- /inside.sh' >/dev/null

for _ in $(seq 180); do podman exec $NAME test -f /tmp/ready 2>/dev/null && break; sleep 1; done
podman exec $NAME test -f /tmp/ready || fail "setup didn't finish"
podman exec $NAME sh -c 'cat /tmp/setup2.out' | grep -q "enable and start cirrusux-gateway.service" || fail "setup didn't start the gateway"
code=""
for _ in $(seq 30); do
	code=$(podman exec $NAME curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8064/ || true)
	[ "$code" != "000" ] && [ -n "$code" ] && break
	sleep 1
done
[ -n "$code" ] && [ "$code" != 000 ] || fail "the gateway doesn't answer on 127.0.0.1:8064"
[ "$(podman exec $NAME ps -o user= -C cirrusux-gateway | tr -d ' ')" = cirrusux ] || fail "the gateway doesn't run as cirrusux"
[ "$(podman exec $NAME stat -c '%U %a' /var/lib/cirrusux-gateway)" = "cirrusux 700" ] || fail "data folder"
[ "$(podman exec $NAME cat /proc/1/comm)" = tini ] || fail "PID 1 isn't tini"
podman exec $NAME sh -c 'sleep 2; cat /var/log/cirrusux-gateway/current' | grep -q "gateway listening" || fail "nothing in the svlogd log"
echo "runit container: tini + runsvdir start the gateway as cirrusux (HTTP $code), logged by svlogd"
