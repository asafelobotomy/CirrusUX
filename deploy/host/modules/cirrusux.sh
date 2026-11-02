# shellcheck shell=bash
# CirrusUX itself: binaries, units, sudoers rule and config files.
#
# Settings (host.env):
#   CIRRUS_ROLE=single        single: gateway and agent here; server: the agent
#                             side of a split setup; gateway: the gateway side
#   CIRRUS_ACTIONS=yes        no: the sudoers rule allows reads only
#   CIRRUS_SERVER_NAME=       written into a new gateway.env (default: host name)
#   CIRRUS_ALLOWED_USERS=     written into a new gateway.env
#   CIRRUS_RELEASE=           folder holding bin/ and deploy/ (default: this checkout)
#
# Config files are created once and then left alone: edit them by hand.
# Services start only when no placeholder (<…>) is left in their config.

module_cirrusux() {
	local role=${CIRRUS_ROLE:-single} rel
	rel=${CIRRUS_RELEASE:-$(cd "$HERE/../.." && pwd)}
	case "$role" in single | server | gateway) ;; *) die "CIRRUS_ROLE must be single, server or gateway" ;; esac
	[ -d "$rel/bin" ] || die "no binaries in $rel/bin: run make build, or set CIRRUS_RELEASE"
	id cirrusux >/dev/null 2>&1 || [ "$MODE" = check ] || die "the cirrusux user is missing: apply the base module first"

	local token=""
	if [ "$role" = single ] && [ ! -e "$ROOT/etc/cirrusux/gateway.env" ] && [ ! -e "$ROOT/etc/cirrusux/agent.env" ]; then
		# Both sides are new here: they share a fresh token.
		token=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
	fi

	local before=$CHANGES units_before
	if [ "$role" != gateway ]; then
		put_file /usr/local/bin/cirrusux-agent 755 root:root <"$rel/bin/cirrusux-agent"
		put_file /usr/local/sbin/cirrusux-helper 755 root:root <"$rel/bin/cirrusux-helper"
		put_file /usr/local/sbin/cirrusux-dockerfilter 755 root:root <"$rel/bin/cirrusux-dockerfilter"
	fi
	if [ "$role" != server ]; then
		put_file /usr/local/bin/cirrusux-gateway 755 root:root <"$rel/bin/cirrusux-gateway"
	fi
	local binaries_changed=$((CHANGES - before))

	units_before=$CHANGES
	local u
	if [ "$role" != gateway ]; then
		for u in cirrusux-agent.service cirrusux-dockerfilter.service cirrusux-dockerfilter.socket; do
			put_file "/etc/systemd/system/$u" 644 root:root <"$rel/deploy/server/$u"
		done
		_cx_sudoers "$rel/deploy/server/sudoers-cirrusux"
	fi
	if [ "$role" != server ]; then
		put_file /etc/systemd/system/cirrusux-gateway.service 644 root:root <"$rel/deploy/gateway/cirrusux-gateway.service"
	fi
	if [ $((CHANGES - units_before)) -gt 0 ]; then
		change "reload systemd units" "—" systemctl daemon-reload
	fi

	_cx_configs "$role" "$rel" "$token"

	if [ "$role" != gateway ]; then
		_cx_service /etc/cirrusux/agent.env cirrusux-dockerfilter.socket cirrusux-agent.service
	fi
	if [ "$role" != server ]; then
		_cx_service /etc/cirrusux/gateway.env cirrusux-gateway.service
	fi
	if [ "$binaries_changed" -gt 0 ]; then
		_cx_restart "$role"
	fi
}

# _cx_sudoers checks the rule with visudo before it goes anywhere near
# /etc/sudoers.d: a broken file there locks sudo out entirely.
_cx_sudoers() {
	local src=$1 tmp
	tmp=$(mktemp)
	if [ "${CIRRUS_ACTIONS:-yes}" = no ]; then
		sed 's/NOPASSWD: CIRRUS_READ, CIRRUS_ACT$/NOPASSWD: CIRRUS_READ/' "$src" >"$tmp"
	else
		cp "$src" "$tmp"
	fi
	visudo -cqf "$tmp" || die "visudo rejects $src; nothing changed"
	put_file /etc/sudoers.d/cirrusux 440 root:root <"$tmp"
	rm -f "$tmp"
}

_cx_configs() {
	local role=$1 rel=$2 token=$3 name
	name=${CIRRUS_SERVER_NAME:-$(hostname -s 2>/dev/null || echo server)}
	if [ "$role" != gateway ]; then
		put_once /etc/cirrusux/helper.env 644 root:root <"$rel/deploy/server/helper.env.example"
		sed -e "s|^CIRRUS_AGENT_TOKEN=.*|CIRRUS_AGENT_TOKEN=${token:-<set in env>}|" \
			"$rel/deploy/server/agent.env.example" | put_once /etc/cirrusux/agent.env 600 cirrusux:cirrusux
	fi
	if [ "$role" != server ]; then
		local setup=split
		[ "$role" = single ] && setup=single
		sed -e "s|^CIRRUS_AGENT_TOKEN=.*|CIRRUS_AGENT_TOKEN=${token:-<set in env>}|" \
			-e "s|^CIRRUS_SERVER_NAME=.*|CIRRUS_SERVER_NAME=$name|" \
			-e "s|^CIRRUS_SETUP=.*|CIRRUS_SETUP=$setup|" \
			-e "s|^CIRRUS_ALLOWED_USERS=.*|CIRRUS_ALLOWED_USERS=${CIRRUS_ALLOWED_USERS:-<your login>}|" \
			"$rel/deploy/gateway/gateway.env.example" | put_once /etc/cirrusux/gateway.env 600 cirrusux:cirrusux
	fi
}

# _cx_service ENVFILE UNIT...: start the units once their config is complete.
_cx_service() {
	local env=$1 u missing=""
	shift
	if [ -f "$ROOT$env" ]; then
		missing=$(placeholders "$ROOT$env")
	elif [ "$MODE" = apply ]; then
		missing="(file missing)"
	fi
	for u in "$@"; do
		if unit_enabled "$u"; then
			ok "$u enabled"
		elif [ -n "$missing" ]; then
			warn "$u stays off until $env is filled in: $missing"
		else
			change "enable and start $u" "systemctl disable --now $u" systemctl enable --now "$u"
		fi
	done
}

_cx_restart() {
	local u units=()
	[ "$1" != gateway ] && units+=(cirrusux-agent.service)
	[ "$1" != server ] && units+=(cirrusux-gateway.service)
	for u in "${units[@]}"; do
		if unit_active "$u"; then
			change "restart $u (new binary)" "install the previous binary and restart" systemctl restart "$u"
		fi
	done
	if unit_active cirrusux-dockerfilter.service; then
		change "stop cirrusux-dockerfilter.service (its socket starts the new one)" "—" \
			systemctl stop cirrusux-dockerfilter.service
	fi
}
