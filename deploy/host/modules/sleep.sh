# shellcheck shell=bash
# Sleep: CirrusUX's own sleep manager, for a server that sleeps when idle.
#
# Settings (host.env):
#   SLEEP_IDLE_MIN=30            quiet minutes before it suspends
#   SLEEP_GRACE_MIN=15           minutes after a wake before it may sleep again
#   SLEEP_WATCH_PORTS=""         only these local ports count; empty: all of them
#   SLEEP_IGNORE_CLIENTS=""      addresses or networks whose connections don't count
#   SLEEP_IGNORE_CLIENTS_PLAY_MB=0   an ignored client pulling more than this
#                                many MB a minute is playing something and
#                                counts after all; 0 off
#   SLEEP_IGNORE_WAKE_GRACE_MIN=0    after a wake only ignored clients used,
#                                sleep this many minutes after the wake; 0 off
#   SLEEP_MAX_LOAD=0             a 5-minute load above this counts; 0 off
#   SLEEP_COUNT_LOGINS=yes       logged-in sessions count
#   SLEEP_WAKE_TIMERS="cirrusux-backup.timer cirrusux-av-scan.timer"
#   SLEEP_RESUME_FIXES=no        yes: after each resume, set Wake-on-LAN on
#                                NET_WOL_IFACE again, restart timesyncd and
#                                log Tailscale's state (cirrusux-resume.service)
#   SLEEP_NO_WAKEUP_DEVICES=""   PCI addresses (lspci -D) that may not wake
#                                the server, e.g. a USB controller
#
# Activity is inbound connections (to the machine and, through conntrack,
# to containers' published ports), logins, busy units (backups, scans, app
# jobs) and stay-awake holds. Outbound connections never count, so the
# agent's reports don't keep it awake. Timers with WakeSystem=yes wake it;
# systemd sets the alarm when it suspends.
#
# Only for the server side of a split setup: in the single setup the
# server runs the panel itself and must stay up.

module_sleep() {
	[ "${CIRRUS_ROLE:-single}" != single ] ||
		die "the sleep module is for a split setup (CIRRUS_ROLE=server): in the single setup the panel runs here and must stay up"
	local rel
	rel=$(release_dir)
	[ -f "$rel/bin/cirrusux-idle" ] || die "no $rel/bin/cirrusux-idle: run make build, or set CIRRUS_RELEASE"
	[[ ${SLEEP_IDLE_MIN:-30} =~ ^[1-9][0-9]*$ ]] || die "SLEEP_IDLE_MIN must be a whole number of minutes"
	[[ ${SLEEP_IGNORE_CLIENTS_PLAY_MB:-0} =~ ^[0-9]+(\.[0-9]+)?$ ]] || die "SLEEP_IGNORE_CLIENTS_PLAY_MB must be a number of MB"
	[[ ${SLEEP_IGNORE_WAKE_GRACE_MIN:-0} =~ ^[0-9]+$ ]] || die "SLEEP_IGNORE_WAKE_GRACE_MIN must be a whole number of minutes"

	need_packages conntrack
	# Byte counts per connection, so an ignored client that streams is seen.
	local sysctl=/etc/sysctl.d/60-cirrusux-conntrack.conf before=$CHANGES
	put_file $sysctl 644 root:root <<'EOF2'
# Written by cirrusux-setup (sleep module): byte counts per connection, for
# SLEEP_IGNORE_CLIENTS_PLAY_MB.
net.netfilter.nf_conntrack_acct=1
EOF2
	if [ "$CHANGES" -ne "$before" ]; then
		change "count bytes per connection now (nf_conntrack_acct)" "sysctl -w net.netfilter.nf_conntrack_acct=0" \
			sysctl -q -p "$ROOT$sysctl"
	fi
	put_file /usr/local/sbin/cirrusux-idle 755 root:root <"$rel/bin/cirrusux-idle"
	put_link /usr/local/sbin/cirrusux-stay-awake cirrusux-idle
	put_link /usr/local/sbin/cirrusux-allow-sleep cirrusux-idle

	put_file /etc/cirrusux/sleep.env 644 root:root <<EOF2
# Written by cirrusux-setup (sleep module) from host.env. The agent reads
# IDLE_MIN, GRACE_MIN and WAKE_TIMERS from here too.
IDLE_MIN=${SLEEP_IDLE_MIN:-30}
GRACE_MIN=${SLEEP_GRACE_MIN:-15}
WATCH_PORTS="${SLEEP_WATCH_PORTS:-}"
IGNORE_CLIENTS="${SLEEP_IGNORE_CLIENTS:-}"
IGNORE_CLIENTS_PLAY_MB=${SLEEP_IGNORE_CLIENTS_PLAY_MB:-0}
IGNORE_WAKE_GRACE_MIN=${SLEEP_IGNORE_WAKE_GRACE_MIN:-0}
MAX_LOAD=${SLEEP_MAX_LOAD:-0}
COUNT_LOGINS=${SLEEP_COUNT_LOGINS:-yes}
WAKE_TIMERS="${SLEEP_WAKE_TIMERS:-cirrusux-backup.timer cirrusux-av-scan.timer}"
EOF2

	local units_before=$CHANGES
	put_file /etc/systemd/system/cirrusux-idle.service 644 root:root <<'EOF2'
# Written by cirrusux-setup (sleep module).
[Unit]
Description=CirrusUX idle check

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/cirrusux-idle check
EOF2
	put_file /etc/systemd/system/cirrusux-idle.timer 644 root:root <<'EOF2'
# Written by cirrusux-setup (sleep module).
[Unit]
Description=CirrusUX idle check, every minute

[Timer]
OnBootSec=2min
OnUnitActiveSec=1min
AccuracySec=5s

[Install]
WantedBy=timers.target
EOF2
	# systemd runs these around every suspend: it records each wake.
	put_file /usr/lib/systemd/system-sleep/cirrusux-idle 755 root:root <<'EOF2'
#!/bin/sh
# Written by cirrusux-setup (sleep module): records suspends and wakes.
exec /usr/local/sbin/cirrusux-idle sleep-hook "$1"
EOF2
	_sleep_resume
	[ $((CHANGES - units_before)) -eq 0 ] || change "reload systemd units" "—" systemctl daemon-reload
	start_unit cirrusux-idle.timer "the idle check (cirrusux-idle.timer)"
	[ "${SLEEP_RESUME_FIXES:-no}" = no ] || start_unit cirrusux-resume.service "the after-resume fixes (cirrusux-resume.service)"
	_sleep_no_wakeup

	helper_module sleep <<'EOF2'
# Written by cirrusux-setup (sleep module).
IDLE_STATE_DIR=/run/cirrusux-idle
IDLE_LOG=/var/log/cirrusux-idle.log
STAY_AWAKE_FILE=/run/cirrusux-idle/hold
STAY_AWAKE_BIN=/usr/local/sbin/cirrusux-stay-awake
ALLOW_SLEEP_BIN=/usr/local/sbin/cirrusux-allow-sleep
EOF2
}

# _sleep_resume: with SLEEP_RESUME_FIXES=yes, a unit run after each resume:
# Wake-on-LAN set again (some cards forget it), the clock synced again, and
# Tailscale's state written to the journal. Each step may fail alone.
_sleep_resume() {
	[ "${SLEEP_RESUME_FIXES:-no}" != no ] || return 0
	local wol=""
	[ -z "${NET_WOL_IFACE:-}" ] || wol="ExecStart=-/usr/sbin/ethtool -s ${NET_WOL_IFACE} wol g"
	put_file /etc/systemd/system/cirrusux-resume.service 644 root:root <<EOF2
# Written by cirrusux-setup (sleep module): fixes after a resume.
[Unit]
Description=CirrusUX after-resume fixes
After=suspend.target

[Service]
Type=oneshot
$wol
ExecStart=-/usr/bin/systemctl restart systemd-timesyncd.service
ExecStart=-/usr/bin/tailscale status --peers=false

[Install]
WantedBy=suspend.target
EOF2
}

# _sleep_no_wakeup: devices in SLEEP_NO_WAKEUP_DEVICES (PCI addresses, e.g.
# a USB controller that wakes the server for nothing) may not wake it: a
# udev rule at every boot, and the setting itself now.
_sleep_no_wakeup() {
	local d rules="" rule=/etc/udev/rules.d/60-cirrusux-no-wakeup.rules
	for d in ${SLEEP_NO_WAKEUP_DEVICES:-}; do
		[[ $d =~ ^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$ ]] || die "SLEEP_NO_WAKEUP_DEVICES: $d isn't a PCI address like 0000:00:14.0 (lspci -D lists them)"
		rules+="ACTION==\"add|change\", SUBSYSTEM==\"pci\", KERNEL==\"$d\", ATTR{power/wakeup}=\"disabled\""$'\n'
	done
	if [ -z "$rules" ]; then
		[ ! -f "$ROOT$rule" ] || change "remove $rule (SLEEP_NO_WAKEUP_DEVICES is empty)" "cirrusux-setup apply sleep" rm -f "$ROOT$rule"
		return 0
	fi
	put_file $rule 644 root:root <<EOF2
# Written by cirrusux-setup (sleep module): these devices may not wake the server.
${rules%$'\n'}
EOF2
	for d in ${SLEEP_NO_WAKEUP_DEVICES}; do
		local f=$ROOT/sys/bus/pci/devices/$d/power/wakeup
		if [ ! -f "$f" ]; then
			warn "no PCI device $d"
		elif [ "$(cat "$f")" = disabled ]; then
			ok "$d may not wake the server"
		else
			change "stop $d waking the server" "echo enabled > /sys/bus/pci/devices/$d/power/wakeup" _sleep_write "$f" disabled
		fi
	done
}

_sleep_write() { echo "$2" >"$1"; }
