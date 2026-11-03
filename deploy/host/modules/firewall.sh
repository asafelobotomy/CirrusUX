# shellcheck shell=bash
# An nftables firewall: LAN and tailnet only, with automatic rollback.
#
# Settings (host.env):
#   FIREWALL_ALLOW="192.168.1.0/24"   networks allowed in (IPv4 and IPv6 CIDRs)
#   FIREWALL_TAILSCALE=yes            also allow the tailnet (tailscale0, 100.64.0.0/10)
#   FIREWALL_PUBLIC_TCP=""            ports open to everyone; empty by default
#   FIREWALL_CONFIRM_SECONDS=120      how long a new ruleset waits for confirmation
#
# Everything lives in one table, inet cirrusux, which leaves Docker's own
# rules alone. Its input chain drops what isn't allowed; its forward chain
# drops connections to containers' published ports from anywhere else, so
# a published port is never reachable from outside the allowed networks.
#
# Applying a changed ruleset starts a timer: unless `cirrusux-setup
# firewall-confirm` runs in time, the previous ruleset comes back, so a bad
# rule can't lock anyone out. Only a confirmed ruleset is loaded at boot.
# Every ruleset is kept in /var/lib/cirrusux/firewall/, never deleted.

FW_TABLE="inet cirrusux"
FW_RULES=/etc/cirrusux/firewall.nft
FW_DIR=/var/lib/cirrusux/firewall
FW_UNIT=cirrusux-firewall.service
FW_TIMER=cirrusux-firewall-rollback

# fw_ruleset prints the table the settings ask for.
fw_ruleset() {
	local v4=() v6=() n
	for n in ${FIREWALL_ALLOW:-}; do
		case "$n" in
		*:*) v6+=("$n") ;;
		*.*) v4+=("$n") ;;
		*) die "FIREWALL_ALLOW: $n is not an address or network" ;;
		esac
	done
	if [ "${FIREWALL_TAILSCALE:-yes}" = yes ]; then
		v4+=(100.64.0.0/10)
		v6+=(fd7a:115c:a1e0::/48)
	fi
	local ports=${FIREWALL_PUBLIC_TCP:-}
	ports=${ports// /, }

	cat <<EOF
# Written by cirrusux-setup (firewall module). Edit host.env, not this file.
table inet cirrusux {
EOF
	if [ ${#v4[@]} -gt 0 ]; then
		printf '\tset allowed4 {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\tauto-merge\n\t\telements = { %s }\n\t}\n' "$(joined "${v4[@]}")"
	fi
	if [ ${#v6[@]} -gt 0 ]; then
		printf '\tset allowed6 {\n\t\ttype ipv6_addr\n\t\tflags interval\n\t\tauto-merge\n\t\telements = { %s }\n\t}\n' "$(joined "${v6[@]}")"
	fi
	cat <<EOF

	chain input {
		type filter hook input priority filter; policy drop;
		ct state established,related accept
		ct state invalid drop
		iif lo accept
		meta l4proto { icmp, ipv6-icmp } accept
		udp dport { 68, 546 } accept comment "DHCP replies"
EOF
	if [ "${FIREWALL_TAILSCALE:-yes}" = yes ]; then
		printf '\t\tiifname "tailscale0" accept\n'
		printf '\t\tudp dport 41641 accept comment "Tailscale direct connections"\n'
	fi
	[ ${#v4[@]} -eq 0 ] || printf '\t\tip saddr @allowed4 accept\n'
	[ ${#v6[@]} -eq 0 ] || printf '\t\tip6 saddr @allowed6 accept\n'
	[ -z "$ports" ] || printf '\t\ttcp dport { %s } accept comment "public"\n' "$ports"
	cat <<EOF
	}

	chain forward {
		type filter hook forward priority filter; policy accept;
		ct state established,related accept
EOF
	if [ "${FIREWALL_TAILSCALE:-yes}" = yes ]; then
		printf '\t\tiifname "tailscale0" accept\n'
	fi
	[ ${#v4[@]} -eq 0 ] || printf '\t\tip saddr @allowed4 accept\n'
	[ ${#v6[@]} -eq 0 ] || printf '\t\tip6 saddr @allowed6 accept\n'
	cat <<EOF
		ct status dnat drop comment "published ports, from anywhere else"
	}
}
EOF
}

# joined A B C prints "A, B, C".
joined() {
	local out=$1
	shift
	for x in "$@"; do out+=", $x"; done
	printf '%s' "$out"
}

# fw_current prints the live table, or nothing if there is none.
fw_current() { nft list table inet cirrusux 2>/dev/null || true; }

module_firewall() {
	need_packages nftables
	if ! command -v nft >/dev/null; then
		[ "$MODE" = check ] && { say "  (the ruleset is worked out once nftables is installed)"; return 0; }
		die "nft is not installed (apt install nftables)"
	fi
	[ -n "${FIREWALL_ALLOW:-}" ] || [ "${FIREWALL_TAILSCALE:-yes}" = yes ] ||
		die "FIREWALL_ALLOW is empty and the tailnet is off: nothing could reach this machine"

	local want
	want=$(mktemp)
	fw_ruleset >"$want"
	nft -c -f "$want" || die "the generated ruleset doesn't load (nft -c); nothing changed"

	# The confirmed file is the reference: the kernel prints rulesets back
	# in its own layout, so comparing with it would always differ.
	local confirmed="$ROOT$FW_RULES"
	if [ -f "$confirmed" ] && cmp -s "$want" "$confirmed" && [ -n "$(fw_current)" ]; then
		ok "ruleset in place and confirmed"
		_fw_boot_unit
		_fw_panel_checks
		rm -f "$want"
		return 0
	fi

	fw_lockout_check
	change "load the new ruleset; it rolls back in ${FIREWALL_CONFIRM_SECONDS:-120} s unless confirmed" \
		"cirrusux-setup firewall-rollback" fw_try "$want"
	if [ "$MODE" = check ]; then
		say "    the new ruleset:"
		sed 's/^/    | /' "$want"
	fi
	rm -f "$want"
}

# fw_lockout_check refuses a ruleset that would cut off the SSH session
# running this, unless the address is allowed. The rollback would undo it,
# but there's no need to find out.
fw_lockout_check() {
	local client n ok
	for client in $(ssh_clients); do
		ok=""
		if [ "${FIREWALL_TAILSCALE:-yes}" = yes ] && in_net "$client" 100.64.0.0/10; then ok=1; fi
		for n in ${FIREWALL_ALLOW:-}; do
			if in_net "$client" "$n"; then ok=1; fi
		done
		[ -n "$ok" ] || die "an SSH session comes from $client, which FIREWALL_ALLOW doesn't include: it would be cut off"
	done
}

# ssh_clients lists the addresses SSH sessions come from. sudo and run0
# drop SSH_CONNECTION, so it's also read from this process's ancestors
# (sudo's parent is the SSH shell) and from the logged-in sessions (run0
# runs outside them): a ruleset must not cut off any of them.
ssh_clients() {
	local pid=$$ env
	{
		[ -n "${SSH_CONNECTION:-}" ] && echo "${SSH_CONNECTION%% *}"
		[ -n "${NO_SESSION_SCAN:-}" ] && exit 0 # tests: only the variable
		while [ "${pid:-1}" -gt 1 ]; do
			env=$(tr '\0' '\n' <"/proc/$pid/environ" 2>/dev/null | sed -n 's/^SSH_CONNECTION=\([^ ]*\).*/\1/p')
			[ -n "$env" ] && echo "$env"
			pid=$(awk '{print $4}' "/proc/$pid/stat" 2>/dev/null)
		done
		who --ips 2>/dev/null | awk '$NF ~ /^[0-9a-fA-F.:]+$/ {print $NF}'
	} | sort -u
}

# in_net ADDR CIDR: IPv4 only; IPv6 sessions are left to the rollback.
in_net() {
	local a=$1 net=${2%/*} bits=${2#*/}
	case "$a$net" in *:*) return 0 ;; esac
	[ "$bits" = "$2" ] && bits=32
	local ai ni mask
	ai=$(ip4int "$a")
	ni=$(ip4int "$net")
	mask=$(((0xffffffff << (32 - bits)) & 0xffffffff))
	[ $((ai & mask)) -eq $((ni & mask)) ]
}

ip4int() {
	local IFS=.
	# shellcheck disable=SC2086 # split the dotted quad
	set -- $1
	echo $((($1 << 24) + ($2 << 16) + ($3 << 8) + $4))
}

# fw_try FILE: keep the live table, load FILE, and start the rollback timer.
fw_try() {
	local new=$1 prev
	# Seconds too: every ruleset is kept, even several in one minute.
	local STAMP
	STAMP=$(date +%Y%m%d-%H%M%S)
	mkdir -p "$ROOT$FW_DIR"
	prev="$ROOT$FW_DIR/$STAMP-before.nft"
	local pending
	pending=$(cat "$ROOT$FW_DIR/pending" 2>/dev/null) || true
	if [ -n "$pending" ] && [ -f "$ROOT$FW_DIR/$pending-before.nft" ]; then
		# A ruleset is still waiting for confirmation: going back means going
		# back past it, to what was there before either.
		cp "$ROOT$FW_DIR/$pending-before.nft" "$prev"
	else
		fw_current >"$prev"
	fi
	cp "$new" "$ROOT$FW_DIR/$STAMP-candidate.nft"
	# One transaction: the table is replaced whole or not at all.
	{
		echo "table $FW_TABLE"
		echo "delete table $FW_TABLE"
		cat "$new"
	} >"$ROOT$FW_DIR/$STAMP-load.nft"
	# The rollback is set up first: if that fails, nothing has changed yet.
	fw_schedule_rollback "$prev"
	echo "$STAMP" >"$ROOT$FW_DIR/pending"
	nft -f "$ROOT$FW_DIR/$STAMP-load.nft"
	say "    Loaded. Check you can still reach this machine, then within ${FIREWALL_CONFIRM_SECONDS:-120} s run:"
	say "      sudo cirrusux-setup firewall-confirm"
	say "    Otherwise the previous rules come back on their own."
}

fw_schedule_rollback() {
	local prev=$1 secs=${FIREWALL_CONFIRM_SECONDS:-120} self
	self=$(readlink -f "$0")
	if command -v systemd-run >/dev/null && [ -z "${NO_SYSTEMD:-}" ]; then
		systemctl stop "$FW_TIMER.timer" 2>/dev/null || true
		systemctl reset-failed "$FW_TIMER.timer" "$FW_TIMER.service" 2>/dev/null || true
		systemd-run --quiet --unit="$FW_TIMER" --on-active="$secs" "$self" firewall-rollback "$prev"
	else
		# Without systemd (tests, containers): a detached sleeper.
		(
			sleep "$secs"
			ROOT=$ROOT NO_CHOWN=${NO_CHOWN:-} "$self" firewall-rollback "$prev"
		) >/dev/null 2>&1 &
		echo $! >"$ROOT$FW_DIR/rollback.pid"
	fi
}

# firewall_confirm keeps the loaded ruleset: it cancels the rollback and
# makes it the one loaded at boot.
firewall_confirm() {
	local pending
	pending=$(cat "$ROOT$FW_DIR/pending" 2>/dev/null) || die "no new ruleset is waiting for confirmation"
	if command -v systemd-run >/dev/null && [ -z "${NO_SYSTEMD:-}" ]; then
		systemctl stop "$FW_TIMER.timer" 2>/dev/null || true
	elif [ -f "$ROOT$FW_DIR/rollback.pid" ]; then
		pkill -P "$(cat "$ROOT$FW_DIR/rollback.pid")" sleep 2>/dev/null || true
		kill "$(cat "$ROOT$FW_DIR/rollback.pid")" 2>/dev/null || true
		rm -f "$ROOT$FW_DIR/rollback.pid"
	fi
	change "confirm the ruleset from $pending" "cirrusux-setup firewall-rollback $FW_DIR/$pending-before.nft" \
		_fw_keep "$ROOT$FW_DIR/$pending-candidate.nft"
	_fw_boot_unit
	_fw_panel_checks
}

# _fw_panel_checks tells the panel's Security page what to check: the
# table's drop policy and the rule that closes published ports.
_fw_panel_checks() {
	helper_module firewall <<'EOF2'
# Written by cirrusux-setup (firewall module), once the ruleset is confirmed.
FIREWALL_CHECKS=inet/cirrusux/input:drop inet/cirrusux/forward:dnat
EOF2
}

_fw_keep() {
	mkdir -p "$(dirname "$ROOT$FW_RULES")"
	backup "$ROOT$FW_RULES"
	install -m 644 "$1" "$ROOT$FW_RULES"
	rm -f "$ROOT$FW_DIR/pending"
}

# firewall_rollback FILE puts the table back as FILE has it (empty: none).
firewall_rollback() {
	local prev=${1:-} manual=""
	if [ -z "$prev" ]; then
		manual=1
		local all=("$ROOT$FW_DIR"/*-before.nft)
		prev=${all[-1]}
	fi
	[ -f "$prev" ] || die "no earlier ruleset to go back to"
	if [ -s "$prev" ]; then
		change "roll the firewall back to $(basename "$prev")" "cirrusux-setup apply firewall" \
			_fw_restore "$prev"
	else
		change "roll the firewall back: remove table $FW_TABLE (there was none before)" "cirrusux-setup apply firewall" \
			_fw_restore ""
	fi
	rm -f "$ROOT$FW_DIR/pending" "$ROOT$FW_DIR/rollback.pid"
	# By hand, going back undoes a confirmed ruleset: the earlier one becomes
	# what loads at boot, and `check` shows host.env's rules as a change again.
	if [ -n "$manual" ]; then
		change "keep $(basename "$prev") as the confirmed ruleset" "cirrusux-setup apply firewall" \
			_fw_keep_back "$prev"
	fi
}

_fw_keep_back() {
	backup "$ROOT$FW_RULES"
	if [ -s "$1" ]; then
		install -m 644 "$1" "$ROOT$FW_RULES"
	else
		rm -f "$ROOT$FW_RULES" # its dated copy stays
		[ -n "${NO_SYSTEMD:-}" ] || systemctl disable "$FW_UNIT" 2>/dev/null || true
	fi
}

_fw_restore() {
	if [ -z "$1" ]; then
		nft delete table inet cirrusux 2>/dev/null || true
		return
	fi
	{
		echo "table $FW_TABLE"
		echo "delete table $FW_TABLE"
		cat "$1"
	} | nft -f -
}

# _fw_boot_unit loads the confirmed ruleset at boot. It only touches its own
# table: Debian's nftables.service flushes every table, Docker's included,
# so it isn't used.
_fw_boot_unit() {
	local before=$CHANGES
	put_file "/etc/systemd/system/$FW_UNIT" 644 root:root <<EOF
# Written by cirrusux-setup (firewall module).
[Unit]
Description=CirrusUX firewall (table inet cirrusux)
Before=network-pre.target docker.service
Wants=network-pre.target
DefaultDependencies=no

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'nft list table inet cirrusux >/dev/null 2>&1 && nft delete table inet cirrusux; exec nft -f $FW_RULES'
ExecStop=-/usr/sbin/nft delete table inet cirrusux

[Install]
WantedBy=sysinit.target
EOF
	if [ "$CHANGES" -ne "$before" ] && unit_enabled "$FW_UNIT"; then
		change "reload systemd units" "—" systemctl daemon-reload
	fi
	if [ -f "$ROOT$FW_RULES" ] && ! unit_enabled "$FW_UNIT"; then
		change "load the confirmed firewall at boot ($FW_UNIT)" "systemctl disable $FW_UNIT" \
			_fw_enable
	fi
}

_fw_enable() {
	systemctl daemon-reload
	systemctl enable "$FW_UNIT"
}
