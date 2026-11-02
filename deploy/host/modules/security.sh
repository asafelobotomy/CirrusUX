# shellcheck shell=bash
# Security: fail2ban for SSH, a weekly ClamAV scan, and the panel's checks.
#
# Settings (host.env):
#   SEC_FAIL2BAN=yes                 ban addresses that keep failing SSH logins
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
		put_file /etc/fail2ban/jail.d/cirrusux.local 644 root:root <<'EOF'
# Written by cirrusux-setup (security module).
[sshd]
enabled = true
backend = systemd
EOF
		if unit_enabled fail2ban.service; then
			ok "fail2ban on"
		else
			change "start fail2ban" "systemctl disable --now fail2ban" systemctl enable --now fail2ban.service
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
		if unit_enabled cirrusux-av-scan.timer; then
			ok "cirrusux-av-scan.timer on"
		else
			change "start cirrusux-av-scan.timer" "systemctl disable --now cirrusux-av-scan.timer" \
				systemctl enable --now cirrusux-av-scan.timer
		fi
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
