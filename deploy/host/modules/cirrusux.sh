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
# Without systemd (runit, see lib.sh's init_system) only the gateway role
# runs: it installs the runit service in /etc/sv/cirrusux-gateway and links
# it into /etc/service ($SVDIR), where runsvdir starts it.
#
# Config files are created once and then left alone: edit them by hand.
# Services start only when no placeholder (<…>) is left in their config.

module_cirrusux() {
	local role=${CIRRUS_ROLE:-single} rel
	rel=$(release_dir)
	case "$role" in single | server | gateway) ;; *) die "CIRRUS_ROLE must be single, server or gateway" ;; esac
	if is_runit && [ "$role" != gateway ]; then
		die "CIRRUS_ROLE=$role needs systemd (the agent's side uses its sockets and jobs); without it only CIRRUS_ROLE=gateway runs"
	fi
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
	# So `sudo cirrusux-setup …`, as setup's own messages say, works anywhere.
	put_link /usr/local/sbin/cirrusux-setup "$rel/deploy/host/cirrusux-setup"

	units_before=$CHANGES
	local u
	local filter_units=$CHANGES
	if [ "$role" != gateway ]; then
		for u in cirrusux-agent.service cirrusux-dockerfilter.service cirrusux-dockerfilter.socket; do
			put_file "/etc/systemd/system/$u" 644 root:root <"$rel/deploy/server/$u"
		done
		filter_units=$((CHANGES - filter_units))
		_cx_sudoers "$rel/deploy/server/sudoers-cirrusux"
	fi
	if [ "$role" != server ] && is_runit; then
		local svc_before=$CHANGES
		put_file "$SV_DIR/cirrusux-gateway/run" 755 root:root <"$rel/deploy/gateway/runit/run"
		put_file "$SV_DIR/cirrusux-gateway/log/run" 755 root:root <"$rel/deploy/gateway/runit/log/run"
		# runsv reads run afresh at each start; a changed one counts on restart.
		if [ "$CHANGES" -ne "$svc_before" ] && unit_active cirrusux-gateway.service; then
			change "restart cirrusux-gateway (new run script)" "—" restart_unit cirrusux-gateway.service
		fi
	elif [ "$role" != server ]; then
		put_file /etc/systemd/system/cirrusux-gateway.service 644 root:root <"$rel/deploy/gateway/cirrusux-gateway.service"
	fi
	if [ $((CHANGES - units_before)) -gt 0 ] && ! is_runit; then
		change "reload systemd units" "—" systemctl daemon-reload
	fi
	# Changed filter units take effect, and a failed socket recovers, only
	# when the socket restarts.
	if [ "$role" != gateway ] && [ "$filter_units" -gt 0 ] && unit_enabled cirrusux-dockerfilter.socket; then
		change "restart cirrusux-dockerfilter.socket" "—" systemctl restart cirrusux-dockerfilter.socket
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
	# The agent reaches the helper through sudo, which a minimal install
	# (a root password and no sudo user) doesn't have.
	need_packages sudo
	if ! command -v visudo >/dev/null; then
		[ "$MODE" = check ] && { rm -f "$tmp"; return 0; }
		die "visudo is missing although sudo was just installed"
	fi
	visudo -cqf "$tmp" || die "visudo rejects $src; nothing changed"
	put_file /etc/sudoers.d/cirrusux 440 root:root <"$tmp"
	rm -f "$tmp"
}

_cx_configs() {
	local role=$1 rel=$2 token=$3 name
	name=${CIRRUS_SERVER_NAME:-$(short_host)}
	if [ "$role" != gateway ]; then
		put_once /etc/cirrusux/helper.env 644 root:root <"$rel/deploy/server/helper.env.example"
		fill_values "CIRRUS_AGENT_TOKEN=${token:-<set in env>}" <"$rel/deploy/server/agent.env.example" |
			put_once /etc/cirrusux/agent.env 600 cirrusux:cirrusux
	fi
	if [ "$role" != server ]; then
		local setup=split
		[ "$role" = single ] && setup=single
		fill_values "CIRRUS_AGENT_TOKEN=${token:-<set in env>}" "CIRRUS_SERVER_NAME=$name" "CIRRUS_SETUP=$setup" \
			"CIRRUS_ALLOWED_USERS=${CIRRUS_ALLOWED_USERS:-<your login>}" <"$rel/deploy/gateway/gateway.env.example" |
			put_once /etc/cirrusux/gateway.env 600 cirrusux:cirrusux
		_cx_gateway_data
	fi
}

# _cx_gateway_data moves an older gateway's files out of /var/lib/cirrusux,
# which belongs to root, into its own folder (CIRRUS_DATA_DIR's default).
_cx_gateway_data() {
	local env="$ROOT/etc/cirrusux/gateway.env" f moved=()
	if grep -qx 'CIRRUS_DATA_DIR=/var/lib/cirrusux' "$env" 2>/dev/null; then
		change "point CIRRUS_DATA_DIR at /var/lib/cirrusux-gateway in gateway.env" \
			"set CIRRUS_DATA_DIR=/var/lib/cirrusux in gateway.env" _cx_new_data_dir "$env"
	fi
	for f in snapshot.json ui.json ui-history audit.jsonl digest.json updates.json assistant-usage.json wallpaper.bin wallpaper.json; do
		[ -e "$STATE_DIR/$f" ] && [ ! -e "$ROOT/var/lib/cirrusux-gateway/$f" ] && moved+=("$f")
	done
	[ ${#moved[@]} -eq 0 ] && return 0
	change "move the gateway's files to /var/lib/cirrusux-gateway: ${moved[*]}" "move them back to /var/lib/cirrusux" \
		_cx_move_data "${moved[@]}"
}

_cx_new_data_dir() {
	backup "$1"
	local tmp
	tmp=$(mktemp)
	fill_values "CIRRUS_DATA_DIR=/var/lib/cirrusux-gateway" <"$1" >"$tmp"
	cat "$tmp" >"$1" # keeps the file's owner and mode
	rm -f "$tmp"
}

_cx_move_data() {
	local f
	mkdir -p "$ROOT/var/lib/cirrusux-gateway"
	for f; do mv "$STATE_DIR/$f" "$ROOT/var/lib/cirrusux-gateway/"; done
	# systemd (or the runit run script) gives the folder to the gateway's
	# user when it next starts.
	if [ -z "${NO_SYSTEMD:-}" ] && unit_active cirrusux-gateway.service; then restart_unit cirrusux-gateway.service; fi
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
			change "enable and start $u" "$(disable_how "$u")" enable_unit "$u"
		fi
	done
}

_cx_restart() {
	local u units=()
	[ "$1" != gateway ] && units+=(cirrusux-agent.service)
	[ "$1" != server ] && units+=(cirrusux-gateway.service)
	for u in "${units[@]}"; do
		if unit_active "$u"; then
			change "restart $u (new binary)" "install the previous binary and restart" restart_unit "$u"
		fi
	done
	if unit_active cirrusux-dockerfilter.service; then
		change "stop cirrusux-dockerfilter.service (its socket starts the new one)" "—" \
			systemctl stop cirrusux-dockerfilter.service
	fi
}
