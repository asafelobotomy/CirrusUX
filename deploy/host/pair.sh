# shellcheck shell=bash
# Connecting the two machines of a split setup. Sourced by cirrusux-setup.
#
#   pair   on the gateway: make the shared token if there isn't one yet, wake
#          by magic packet unless another way is set, and print a join code
#   join   on the server: enter that code; agent.env gets the gateway's
#          address and the token, and the agent starts
#
# The code holds the token, so treat it like a password: it's printed once,
# never logged, and read back without echo. The gateway's address is its
# Tailscale name (CIRRUS_PAIR_URL in host.env sets another).

PAIR_PREFIX=cirrusux1:
# The gateway's address: a name or IPv4 address, an optional port and path.
PAIR_URL_RE='^https?://[A-Za-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._/-]*)?/agent/snapshot$'

# _env_set FILE KEY VALUE: set KEY in an env file, in place (keeping its
# owner and mode), uncommenting `#KEY=` or appending when it isn't there.
# Done in the shell, so the value never shows in the process list.
_env_set() {
	local f=$1 key=$2 val=$3 line done=no tmp
	tmp=$(mktemp)
	while IFS= read -r line || [ -n "$line" ]; do
		if [ "$done" = no ] && { [[ $line == "$key="* ]] || [[ $line == "#$key="* ]]; }; then
			line="$key=$val"
			done=yes
		fi
		printf '%s\n' "$line"
	done <"$f" >"$tmp"
	[ "$done" = yes ] || printf '%s=%s\n' "$key" "$val" >>"$tmp"
	cat "$tmp" >"$f"
	rm -f "$tmp"
}

# _env_get FILE KEY: KEY's value, or nothing.
_env_get() { sed -n "s/^$2=//p" "$1" 2>/dev/null | tail -1; }

# _env_change FILE WHAT KEY=VALUE...: one logged change, backed up first.
_env_change() {
	local f=$1 what=$2
	shift 2
	change "$what" "restore $f.bak-$STAMP" _env_apply "$f" "$@"
}

_env_apply() {
	local f=$1 kv
	shift
	backup "$f"
	for kv; do _env_set "$f" "${kv%%=*}" "${kv#*=}"; done
}

# _restart_or_start UNIT: restart a running unit for its new settings, or
# apply the cirrusux module, which starts it once nothing is left to fill in.
_restart_or_start() {
	if unit_active "$1"; then
		change "restart $1 (new settings)" "—" restart_unit "$1"
	else
		"$HERE/cirrusux-setup" apply cirrusux
	fi
}

setup_pair() {
	[ "${CIRRUS_ROLE:-single}" = gateway ] || die "pair runs on the gateway of a split setup (CIRRUS_ROLE=gateway); a single setup needs no pairing"
	local env=$ROOT/etc/cirrusux/gateway.env
	[ -f "$env" ] || die "no $env yet: run sudo cirrusux-setup apply first"

	local url=${CIRRUS_PAIR_URL:-} dns
	if [ -z "$url" ]; then
		dns=$(ts_self_dns)
		[ -n "$dns" ] || die "couldn't find this machine's Tailscale name: log in to Tailscale, or set CIRRUS_PAIR_URL=https://…/agent/snapshot in host.env"
		url=https://$dns/agent/snapshot
		[ "${CIRRUS_TAILSCALE_SERVE:-no}" = yes ] ||
			warn "the server reaches the gateway at $url only through tailscale serve: set CIRRUS_TAILSCALE_SERVE=yes in host.env, or publish it yourself"
	fi
	[[ $url =~ $PAIR_URL_RE ]] || die "CIRRUS_PAIR_URL must end in /agent/snapshot: $url"

	local token sets=()
	token=$(_env_get "$env" CIRRUS_AGENT_TOKEN)
	if ! [[ $token =~ ^[A-Za-z0-9_-]{32,}$ ]]; then
		token=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
		sets+=("CIRRUS_AGENT_TOKEN=$token")
	fi
	if [ -z "$(_env_get "$env" CIRRUS_WAKE)" ]; then
		sets+=("CIRRUS_WAKE=wol")
	fi
	if [ ${#sets[@]} -gt 0 ]; then
		local what="set in gateway.env:"
		local kv
		for kv in "${sets[@]}"; do what="$what ${kv%%=*}"; done
		_env_change "$env" "$what" "${sets[@]}"
		[ -n "${NO_CHOWN:-}" ] || chown cirrusux:cirrusux "$env"
		_restart_or_start cirrusux-gateway.service
	fi

	local code
	code=$PAIR_PREFIX$(printf '%s %s' "$url" "$token" | base64 -w0)
	say ""
	say "On the server, run  sudo cirrusux-setup join  and enter this code."
	say "It holds the shared token: don't post it anywhere."
	say ""
	say "  $code"
	say ""
	if [ "$(_env_get "$env" CIRRUS_WAKE)" = wol ] && [ -z "$(_env_get "$env" CIRRUS_WAKE_MAC)" ]; then
		say "Waking: by magic packet, to the MAC address the server reports once it's joined."
	fi
}

setup_join() {
	[ "${CIRRUS_ROLE:-single}" = server ] || die "join runs on the server of a split setup (CIRRUS_ROLE=server)"
	local env=$ROOT/etc/cirrusux/agent.env
	[ -f "$env" ] || die "no $env yet: run sudo cirrusux-setup apply first"

	local code="" b64 plain url token
	if [ -t 0 ]; then
		read -r -s -p "Join code (from cirrusux-setup pair on the gateway): " code
		echo
	else
		IFS= read -r code || true
	fi
	code=${code//[[:space:]]/}
	[[ $code == "$PAIR_PREFIX"* ]] || die "that isn't a join code: it starts with $PAIR_PREFIX"
	b64=${code#"$PAIR_PREFIX"}
	[[ $b64 =~ ^[A-Za-z0-9+/]+=*$ ]] || die "the join code is damaged: copy it again"
	plain=$(base64 -d <<<"$b64" 2>/dev/null) || die "the join code is damaged: copy it again"
	url=${plain%% *}
	token=${plain#* }
	[[ $url =~ $PAIR_URL_RE ]] || die "the join code is damaged: copy it again"
	[[ $token =~ ^[A-Za-z0-9_-]{32,}$ ]] || die "the join code is damaged: copy it again"

	if [ "$(_env_get "$env" CIRRUS_GATEWAY_URL)" = "$url" ] && [ "$(_env_get "$env" CIRRUS_AGENT_TOKEN)" = "$token" ]; then
		ok "agent.env already reports to $url"
	else
		_env_change "$env" "set in agent.env: CIRRUS_GATEWAY_URL=$url, CIRRUS_AGENT_TOKEN" \
			"CIRRUS_GATEWAY_URL=$url" "CIRRUS_AGENT_TOKEN=$token"
		[ -n "${NO_CHOWN:-}" ] || chown cirrusux:cirrusux "$env"
		_restart_or_start cirrusux-agent.service
	fi
	say ""
	say "Joined. The agent reports to $url; the panel is at ${url%/agent/snapshot}/"
}
