# shellcheck shell=bash
# Security: fail2ban for SSH, a weekly ClamAV scan, and the panel's checks.
#
# Settings (host.env):
#   SEC_FAIL2BAN=yes                 ban addresses that keep failing SSH logins
#   SEC_FAIL2BAN_IGNOREIP="127.0.0.1/8 ::1"   never banned (add the tailnet:
#                                    100.64.0.0/10)
#   SEC_FAIL2BAN_BANTIME=600         seconds banned (or 10m, 1h…; -1 forever)
#   SEC_FAIL2BAN_FINDTIME=600        failures count within this window
#   SEC_FAIL2BAN_MAXRETRY=5          failures before a ban
#   SEC_AV=yes                       weekly virus scan
#   SEC_AV_PATHS="/home /srv /usr/local /tmp /var/tmp"
#   SEC_AV_SCHEDULE="Sun *-*-* 04:00"
#
# The scan updates its signatures just before it runs, instead of a
# freshclam daemon checking all day: nothing runs in the background, and a
# sleeping server isn't kept awake. The scan's timer wakes it
# (WakeSystem=yes). Infected files are reported, never deleted or moved.

module_security() {
	if [ "${SEC_FAIL2BAN:-yes}" = yes ]; then
		# Debian logs to the journal, so the jail reads it there.
		need_packages fail2ban python3-systemd
		local ignore=${SEC_FAIL2BAN_IGNOREIP:-127.0.0.1/8 ::1} bantime=${SEC_FAIL2BAN_BANTIME:-600}
		local findtime=${SEC_FAIL2BAN_FINDTIME:-600} maxretry=${SEC_FAIL2BAN_MAXRETRY:-5} w
		for w in $ignore; do
			[[ $w =~ ^[0-9a-fA-F.:]+(/[0-9]+)?$ ]] || die "SEC_FAIL2BAN_IGNOREIP: $w isn't an address or network"
		done
		[[ $bantime =~ ^-?[0-9]+[smhdw]?$ && $findtime =~ ^[0-9]+[smhdw]?$ ]] ||
			die "SEC_FAIL2BAN_BANTIME and _FINDTIME are seconds, or a number with s, m, h, d or w"
		[[ $maxretry =~ ^[1-9][0-9]*$ ]] || die "SEC_FAIL2BAN_MAXRETRY must be a whole number"
		local jail_before=$CHANGES
		# nftables bans: the firewall module's table is nftables too, and
		# Debian 13 has no iptables by default.
		put_file /etc/fail2ban/jail.d/cirrusux.local 644 root:root <<EOF
# Written by cirrusux-setup (security module).
[DEFAULT]
banaction = nftables-multiport
banaction_allports = nftables-allports
ignoreip = $ignore
bantime = $bantime
findtime = $findtime
maxretry = $maxretry

[sshd]
enabled = true
backend = systemd
EOF
		if ! unit_enabled fail2ban.service; then
			change "start fail2ban" "systemctl disable --now fail2ban" systemctl enable --now fail2ban.service
		elif [ "$CHANGES" -ne "$jail_before" ] || ! unit_active fail2ban.service; then
			# A changed jail only counts once fail2ban reads it again.
			change "restart fail2ban with the jail" "—" systemctl reload-or-restart fail2ban.service
		else
			ok "fail2ban on"
		fi
	fi

	if [ "${SEC_AV:-yes}" = yes ]; then
		need_packages clamav
		if unit_enabled clamav-freshclam.service; then
			change "stop the freshclam daemon (the scan updates signatures itself)" \
				"systemctl enable --now clamav-freshclam" systemctl disable --now clamav-freshclam.service
		else
			ok "no freshclam daemon"
		fi
		local units_before=$CHANGES
		put_file /etc/systemd/system/cirrusux-av-scan.service 644 root:root <<EOF
# Written by cirrusux-setup (security module).
[Unit]
Description=CirrusUX weekly virus scan
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
LogsDirectory=cirrusux
ExecStartPre=-/usr/bin/freshclam --quiet
ExecStart=/usr/bin/clamscan --recursive --infected --cross-fs=no --log=/var/log/cirrusux/av-scan.log ${SEC_AV_PATHS:-/home /srv /usr/local /tmp /var/tmp}
# 1 means something was found: reported in the log and the panel, not a failure.
SuccessExitStatus=1
Nice=19
IOSchedulingClass=idle
EOF
		put_file /etc/systemd/system/cirrusux-av-scan.timer 644 root:root <<EOF
# Written by cirrusux-setup (security module).
[Unit]
Description=CirrusUX weekly virus scan, on a schedule

[Timer]
OnCalendar=${SEC_AV_SCHEDULE:-Sun *-*-* 04:00}
Persistent=yes
WakeSystem=yes

[Install]
WantedBy=timers.target
EOF
		[ $((CHANGES - units_before)) -eq 0 ] || change "reload systemd units" "—" systemctl daemon-reload
		start_unit cirrusux-av-scan.timer
		helper_module av <<'EOF'
# Written by cirrusux-setup (security module).
AV_LOG=/var/log/cirrusux/av-scan.log
EOF
	fi

	# The panel checks the jail; the firewall module adds its own checks
	# once its ruleset is confirmed (helper.d/firewall.env).
	{
		echo "# Written by cirrusux-setup (security module)."
		[ "${SEC_FAIL2BAN:-yes}" = yes ] && echo "FAIL2BAN_JAIL=sshd"
		true
	} | helper_module security
}
