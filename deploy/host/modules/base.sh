# shellcheck shell=bash
# The basics: the cirrusux user, the time zone, no automatic updates.
#
# Settings (host.env):
#   HOST_TZ=Europe/London        IANA zone; empty leaves it alone
#   HOST_AUTO_UPDATES=no         no: switch off apt's daily upgrade timer
#
# Without systemd (runit), the zone is set through /etc/localtime and
# /etc/timezone, and there are no apt timers to switch off.
#
# SSH is left alone here: how people log in stays the owner's business. The
# opt-in ssh module can restrict who logs in from where, and nothing more.

module_base() {
	if id cirrusux >/dev/null 2>&1; then
		ok "user cirrusux"
	else
		change "create system user cirrusux" "userdel cirrusux" \
			useradd --system --no-create-home --shell /usr/sbin/nologin cirrusux
	fi

	local tz=${HOST_TZ:-}
	if [ -n "$tz" ]; then
		[ -e "$ROOT/usr/share/zoneinfo/$tz" ] || die "HOST_TZ: unknown zone $tz"
		local now
		now=$(readlink "$ROOT/etc/localtime" 2>/dev/null | sed 's|.*zoneinfo/||' || true)
		if [ "$now" = "$tz" ]; then
			ok "time zone $tz"
		elif is_runit; then
			change "set the time zone to $tz (was ${now:-unset})" "link /etc/localtime to /usr/share/zoneinfo/${now:-UTC}" \
				_set_tz_files "$tz"
		else
			change "set the time zone to $tz (was ${now:-unset})" "timedatectl set-timezone ${now:-UTC}" \
				timedatectl set-timezone "$tz"
		fi
	fi

	# apt's automatic upgrades are systemd timers: without systemd there are none.
	if [ "${HOST_AUTO_UPDATES:-no}" = no ] && ! is_runit; then
		local t
		for t in apt-daily-upgrade.timer unattended-upgrades.service; do
			if unit_enabled "$t"; then
				change "switch off $t (no automatic updates)" "systemctl enable --now $t" \
					systemctl disable --now "$t"
			else
				ok "$t off"
			fi
		done
	fi

	if [ -d "$ROOT/etc/cirrusux" ] && [ "$(file_mode "$ROOT/etc/cirrusux")" = 750 ]; then
		ok "/etc/cirrusux"
	elif [ -d "$ROOT/etc/cirrusux" ]; then
		change "make /etc/cirrusux root:cirrusux 750 (was $(file_mode "$ROOT/etc/cirrusux"))" "chmod $(file_mode "$ROOT/etc/cirrusux") /etc/cirrusux" \
			_mkconfdir
	else
		change "create /etc/cirrusux (root:cirrusux 750)" "rm -r /etc/cirrusux" \
			_mkconfdir
	fi
}

# _set_tz_files ZONE: what timedatectl does, by hand (no systemd).
_set_tz_files() {
	backup "$ROOT/etc/timezone"
	ln -sfn "/usr/share/zoneinfo/$1" "$ROOT/etc/localtime"
	echo "$1" >"$ROOT/etc/timezone"
}

_mkconfdir() {
	mkdir -p "$ROOT/etc/cirrusux"
	chmod 750 "$ROOT/etc/cirrusux"
	[ -n "${NO_CHOWN:-}" ] || chown root:cirrusux "$ROOT/etc/cirrusux"
}
