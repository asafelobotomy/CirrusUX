# shellcheck shell=bash
# Backups: an age-encrypted system backup on a timer, shown in the panel.
#
# Settings (host.env):
#   BACKUP_AGE_RECIPIENT=age1…   public key(s) to encrypt to (required); keep
#                                the private key off this machine
#   BACKUP_DIR=/srv/backups      where archives go
#   BACKUP_PATHS="/etc /root /home /usr/local /var/lib/cirrusux /var/lib/cirrusux-gateway /srv/cirrusux"
#   BACKUP_EXCLUDE=""            paths or patterns to leave out
#   BACKUP_SCHEDULE="*-*-* 03:30"   systemd OnCalendar
#   BACKUP_KEEP=7                newest archives to keep; 0 keeps all (e.g. 14
#                                for two weeks of nightly backups)
#   BACKUP_REQUIRE_MOUNT=""      a mount point BACKUP_DIR must be on, e.g. an
#                                external disk's /mnt/media: without the disk
#                                the backup refuses to run, rather than fill
#                                the system disk
#
# The timer wakes a sleeping server (WakeSystem=yes) and runs late if it was
# off at the time (Persistent=yes). Media isn't in the default paths.

module_backup() {
	local rcpt=${BACKUP_AGE_RECIPIENT:-}
	[ -n "$rcpt" ] || die "BACKUP_AGE_RECIPIENT is required: an age public key (age1…), from age-keygen on another machine"
	local r
	for r in $rcpt; do
		[[ $r =~ ^age1[0-9a-z]{50,}$ ]] || die "BACKUP_AGE_RECIPIENT: $r isn't an age public key (age1…)"
	done
	local dir=${BACKUP_DIR:-/srv/backups} keep=${BACKUP_KEEP:-7}
	[[ $keep =~ ^[0-9]+$ ]] || die "BACKUP_KEEP must be a number"
	valid_path "$dir" || die "BACKUP_DIR must be an absolute path without spaces, special characters, \"..\" or a trailing /"
	local mount=${BACKUP_REQUIRE_MOUNT:-}
	if [ -n "$mount" ]; then
		valid_path "$mount" || die "BACKUP_REQUIRE_MOUNT must be an absolute path without spaces, special characters, \"..\" or a trailing /"
		[[ $dir == "$mount" || $dir == "$mount"/* ]] || die "BACKUP_DIR ($dir) isn't inside BACKUP_REQUIRE_MOUNT ($mount)"
	fi
	local prefix
	prefix="$(short_host)-system-"

	need_packages age zstd

	put_file /usr/local/sbin/cirrusux-backup 755 root:root <"$HERE/files/cirrusux-backup"
	put_file /etc/cirrusux/backup.env 600 root:root <<EOF
# Written by cirrusux-setup (backup module) from host.env.
BACKUP_DIR="$dir"
BACKUP_PREFIX="$prefix"
BACKUP_AGE_RECIPIENT="$rcpt"
BACKUP_PATHS="${BACKUP_PATHS:-$DEFAULT_BACKUP_PATHS}"
BACKUP_EXCLUDE="${BACKUP_EXCLUDE:-}"
BACKUP_KEEP=$keep
BACKUP_REQUIRE_MOUNT="$mount"
EOF

	local units_before=$CHANGES
	# RequiresMountsFor mounts whatever BACKUP_DIR is on (from fstab) first;
	# the script still checks, for a disk that isn't in fstab.
	put_file /etc/systemd/system/cirrusux-backup.service 644 root:root <<EOF
# Written by cirrusux-setup (backup module).
[Unit]
Description=CirrusUX system backup
After=local-fs.target
RequiresMountsFor=$dir

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/cirrusux-backup
Nice=10
IOSchedulingClass=idle
EOF
	put_file /etc/systemd/system/cirrusux-backup.timer 644 root:root <<EOF
# Written by cirrusux-setup (backup module).
[Unit]
Description=CirrusUX system backup, on a schedule

[Timer]
OnCalendar=${BACKUP_SCHEDULE:-*-*-* 03:30}
Persistent=yes
WakeSystem=yes

[Install]
WantedBy=timers.target
EOF
	[ $((CHANGES - units_before)) -eq 0 ] || change "reload systemd units" "—" systemctl daemon-reload
	start_unit cirrusux-backup.timer

	# Tell the panel's helper where to look.
	helper_module backup <<EOF
# Written by cirrusux-setup (backup module).
BACKUP_UNIT=cirrusux-backup.service
BACKUP_DIR=$dir
BACKUP_PREFIX=$prefix
EOF
}
