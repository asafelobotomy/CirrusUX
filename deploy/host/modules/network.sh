# shellcheck shell=bash
# Network: Wake-on-LAN that survives reboots, and Tailscale running at boot.
#
# Settings (host.env):
#   NET_WOL_IFACE=enp1s0     wired interface to wake on; empty skips Wake-on-LAN
#   NET_TAILSCALE=yes        check that tailscaled starts at boot and is logged in
#   NET_TAILSCALE_SET=""         preferences for `tailscale set`, e.g.
#                                "--accept-dns=false --accept-routes"
#   NET_TAILSCALED_FLAGS=""      tailscaled's flags, e.g. "--no-logs-no-support"
#                                (FLAGS= in /etc/default/tailscaled; restarts it)
#
# Logging in to Tailscale needs a browser, so setup only reports it. Without
# systemd (runit), tailscaled is never started or restarted, and
# Wake-on-LAN is skipped.

module_network() {
	local ifc=${NET_WOL_IFACE:-}
	if [ -n "$ifc" ] && is_runit; then
		warn "NET_WOL_IFACE: Wake-on-LAN is kept on by a systemd .link file, so it's skipped without systemd"
		ifc=""
	fi
	if [ -n "$ifc" ]; then
		need_packages ethtool
		local mac
		mac=$(cat "$ROOT/sys/class/net/$ifc/address" 2>/dev/null) || die "NET_WOL_IFACE: no interface named $ifc"
		# Matching by MAC replaces the default .link file for this card, so
		# the naming policies are repeated to keep its name as it is.
		put_file /etc/systemd/network/50-cirrusux-wol.link 644 root:root <<EOF2
# Written by cirrusux-setup (network module): Wake-on-LAN for $ifc.
[Match]
MACAddress=$mac

[Link]
NamePolicy=keep kernel database onboard slot path
AlternativeNamesPolicy=database onboard slot path
MACAddressPolicy=persistent
WakeOnLan=magic
EOF2
		if ethtool "$ifc" 2>/dev/null | grep -qE '^\s*Wake-on:\s*g'; then
			ok "Wake-on-LAN on for $ifc now"
		elif ethtool "$ifc" 2>/dev/null | grep -qE 'Supports Wake-on:.*g'; then
			change "switch on Wake-on-LAN for $ifc now (the .link file keeps it after reboots)" \
				"ethtool -s $ifc wol d" ethtool -s "$ifc" wol g
		else
			warn "$ifc doesn't report magic-packet support: check the BIOS/UEFI setting for wake on LAN"
		fi
	fi

	if [ "${NET_TAILSCALE:-yes}" = yes ]; then
		if ! command -v tailscale >/dev/null; then
			warn "Tailscale isn't installed: see https://tailscale.com/download/linux"
			return 0
		fi
		if is_runit; then
			# The machine's own tailscaled (often userspace mode, started its
			# own way) is never started, restarted or duplicated.
			ok "tailscaled left as it is (no systemd)"
		elif unit_enabled tailscaled.service && unit_active tailscaled.service; then
			ok "tailscaled starts at boot"
		else
			change "start tailscaled now and at boot" "systemctl disable --now tailscaled" \
				systemctl enable --now tailscaled.service
		fi
		if tailscale status --json 2>/dev/null | grep -q '"BackendState": *"Running"'; then
			ok "Tailscale logged in"
		else
			warn "Tailscale isn't logged in: run 'sudo tailscale up' once"
		fi
		_net_tailscale_set
		_net_tailscaled_flags
	fi
}

# _net_flags NAME VALUE: dies unless VALUE is only --flag or --flag=value
# words, so nothing but flags reaches tailscale.
_net_flags() {
	local w
	for w in $2; do
		[[ $w =~ ^--[a-z0-9-]+(=[A-Za-z0-9.,:/@_+-]*)?$ ]] || die "$1: \"$w\" isn't a --flag or --flag=value"
	done
}

# NET_TAILSCALE_SET: preferences, applied with `tailscale set`. What was
# applied is kept in /var/lib/cirrusux/tailscale-set, and the preferences
# from before the first change in tailscale-prefs.before, for the undo.
_net_tailscale_set() {
	local want=${NET_TAILSCALE_SET:-} applied=/var/lib/cirrusux/tailscale-set have=""
	[ -n "$want" ] || return 0
	_net_flags NET_TAILSCALE_SET "$want"
	[ -f "$ROOT$applied" ] && have=$(cat "$ROOT$applied")
	if [ "$have" = "$want" ]; then
		ok "tailscale set $want"
		return 0
	fi
	change "tailscale set $want" "tailscale set back as in /var/lib/cirrusux/tailscale-prefs.before${have:+ (applied before: $have)}" \
		_net_apply_set "$want" "$ROOT$applied"
}

_net_apply_set() {
	local before=$ROOT/var/lib/cirrusux/tailscale-prefs.before
	mkdir -p "$(dirname "$2")"
	[ -e "$before" ] || tailscale debug prefs >"$before" 2>/dev/null || true
	# shellcheck disable=SC2086 # one word per flag, checked by _net_flags
	tailscale set $1
	echo "$1" >"$2"
}

# NET_TAILSCALED_FLAGS: tailscaled's own flags (FLAGS= in
# /etc/default/tailscaled, which Debian's unit reads), then a restart.
_net_tailscaled_flags() {
	local want=${NET_TAILSCALED_FLAGS:-} f=/etc/default/tailscaled
	[ -n "$want" ] || return 0
	_net_flags NET_TAILSCALED_FLAGS "$want"
	if is_runit; then
		warn "NET_TAILSCALED_FLAGS: tailscaled is never restarted without systemd; set its flags where it's started"
		return 0
	fi
	local before=$CHANGES content
	if [ -f "$ROOT$f" ]; then
		content=$(grep -v '^FLAGS=' "$ROOT$f"; echo "FLAGS=\"$want\"")
	else
		content=$(printf '# Set by cirrusux-setup (network module).\nPORT="41641"\nFLAGS="%s"' "$want")
	fi
	put_file $f 644 root:root <<<"$content"
	if [ "$CHANGES" -ne "$before" ]; then
		change "restart tailscaled (new flags; Tailscale connections drop for a moment)" "restore $f and restart tailscaled" \
			restart_unit tailscaled.service
	fi
}
