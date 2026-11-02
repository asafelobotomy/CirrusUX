#!/bin/bash
# capture.sh — copies cloud64's own setup off the server before the
# reinstall (ROADMAP §3.1). Read-only: it changes nothing on cloud64 and
# never runs cloud64-idle-check (its --dry-run writes state).
#
#   sudo bash capture.sh            # writes ~/cloud64-capture-<date>.tar.gz
#
# Then copy the archive to the admin machine and delete it from cloud64.
# The archive is mode 600. Files that look like they hold secrets are
# listed in SECRETS-CHECK.txt (names only, never values): look at those
# before the archive goes anywhere else.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
	echo "run with sudo: some files are root-only" >&2
	exit 1
fi

owner=${SUDO_USER:-root}
home=$(getent passwd "$owner" | cut -d: -f6)
stamp=$(date +%Y%m%d-%H%M)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
out="$work/cloud64-capture-$stamp"
mkdir -p "$out/files" "$out/commands"

# Files and folders worth keeping. Missing ones are noted, not fatal.
paths=(
	/usr/local/bin /usr/local/sbin
	/etc/cloud64-idle.conf
	/etc/systemd/system /etc/systemd/sleep.conf /etc/systemd/sleep.conf.d /etc/systemd/logind.conf.d
	/etc/sudoers.d
	/etc/fail2ban/jail.local /etc/fail2ban/jail.d
	/etc/clamav/clamd.conf /etc/clamav/freshclam.conf
	/etc/fstab /etc/crypttab
	/etc/network/interfaces /etc/systemd/network /etc/NetworkManager/system-connections
	/etc/ssh/sshd_config /etc/ssh/sshd_config.d
	/etc/docker/daemon.json
	/etc/default/tailscaled
	/etc/apt/apt.conf.d
	/var/spool/cron/crontabs
)
for p in "${paths[@]}"; do
	if [ -e "$p" ]; then
		cp -a --parents "$p" "$out/files/"
	else
		echo "$p" >>"$out/MISSING.txt"
	fi
done

# Never keep keys or env files that may hold passwords, even if a path
# above swept them up.
find "$out/files" \( -name '*.env' -o -name '*.key' -o -name '*.pem' -o -name 'id_*' \
	-o -path '*/system-connections/*' -o -name '*.age' \) -print -delete 2>/dev/null |
	sed "s|^$out/files||" >"$out/LEFT-OUT.txt" || true

# 1Panel's compose files: a reference for mounts, devices and groups.
if [ -d /opt/1panel/apps ]; then
	find /opt/1panel/apps -maxdepth 4 -name 'docker-compose.yml' -print0 |
		while IFS= read -r -d '' f; do cp --parents "$f" "$out/files/"; done
fi

# Read-only commands: state that isn't in a file.
run() {
	local name=$1
	shift
	"$@" >"$out/commands/$name.txt" 2>&1 || echo "(exit $?)" >>"$out/commands/$name.txt"
}
run os-release cat /etc/os-release
run uname uname -a
run timers systemctl list-timers --all --no-pager
run units systemctl list-unit-files --no-pager
run failed systemctl --failed --no-pager
run sleep-state cat /sys/power/mem_sleep
run wakeup-sources cat /proc/acpi/wakeup
run iptables iptables-save
run ip6tables ip6tables-save
run nft nft list ruleset
run docker-ps docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
run lsblk lsblk -o NAME,SIZE,FSTYPE,MOUNTPOINT,MODEL,TRAN
run df df -h
# shellcheck disable=SC2016 # dpkg-query's own format, not shell
run packages dpkg-query -W -f '${Package}\t${Version}\n'
run idle-state ls -la /run/cloud64-idle
run idle-log-tail tail -n 300 /var/log/cloud64-idle.log
for dev in /sys/class/net/*; do
	n=$(basename "$dev")
	[ "$n" = lo ] && continue
	run "ethtool-$n" ethtool "$n"
done

# Name the files that look like they hold secrets; never print the values.
grep -rIlE '(PASS(WORD)?|SECRET|TOKEN|API_?KEY|PRIVATE KEY)[[:space:]]*[:=]' "$out" 2>/dev/null |
	sed "s|^$out/||" >"$out/SECRETS-CHECK.txt" || true

archive="$home/cloud64-capture-$stamp.tar.gz"
tar -C "$work" -czf "$archive" "cloud64-capture-$stamp"
chown "$owner": "$archive"
chmod 600 "$archive"
echo "wrote $archive ($(du -h "$archive" | cut -f1))"
if [ -s "$out/SECRETS-CHECK.txt" ]; then
	echo "check these files for secrets before copying it anywhere else:"
	sed 's/^/  /' "$out/SECRETS-CHECK.txt"
fi
