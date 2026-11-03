# shellcheck shell=bash
# Network: Wake-on-LAN that survives reboots, and Tailscale running at boot.
#
# Settings (host.env):
#   NET_WOL_IFACE=enp1s0     wired interface to wake on; empty skips Wake-on-LAN
#   NET_TAILSCALE=yes        check that tailscaled starts at boot and is logged in
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
	fi
}
