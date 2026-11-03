# shellcheck shell=bash
# Apps: the app store's runtime, folders and user.
#
# Settings (host.env):
#   APPS_RUNTIME=podman            podman (Quadlet units) or docker (compose)
#   APPS_DIR=/srv/cirrusux/apps    each app's own folders
#   APPS_ARCHIVE=/srv/cirrusux/archive   removed apps' folders go here
#   APPS_MEDIA_ROOTS="/srv/media"  folders media may be chosen from
#   APPS_BIND=""                   publish apps' ports on this address only
#                                  (e.g. the LAN address); empty: all
#
# Apps run as the cirrusux-apps user where they can. With Podman, the
# Docker filter is pointed at Podman's API socket, so the panel's container
# list and start/stop/restart work the same on either runtime.

module_apps() {
	local rt=${APPS_RUNTIME:-podman} dir=${APPS_DIR:-/srv/cirrusux/apps} archive=${APPS_ARCHIVE:-/srv/cirrusux/archive}
	local roots=${APPS_MEDIA_ROOTS:-/srv/media} r
	case "$rt" in podman | docker) ;; *) die "APPS_RUNTIME must be podman or docker" ;; esac
	[ -z "${APPS_BIND:-}" ] || valid_ip "$APPS_BIND" || die "APPS_BIND must be an IP address"
	for r in "$dir" "$archive" $roots; do
		valid_path "$r" || die "$r: an absolute path without spaces, special characters, \"..\" or a trailing /"
	done

	if [ "$rt" = podman ]; then
		# Installed without "recommends", so name what rootful Podman needs:
		# netavark calls nft for container networking, aardvark-dns names.
		need_packages podman nftables aardvark-dns
		start_unit podman.socket "Podman's API socket (podman.socket)"
		local before=$CHANGES
		put_file /etc/systemd/system/cirrusux-dockerfilter.service.d/podman.conf 644 root:root <<'EOF2'
# Written by cirrusux-setup (apps module): the filter talks to Podman.
[Unit]
After=podman.socket
Wants=podman.socket

[Service]
ExecStart=
ExecStart=/usr/local/sbin/cirrusux-dockerfilter -upstream /run/podman/podman.sock
ReadWritePaths=
ReadWritePaths=/run/podman/podman.sock
EOF2
		if [ "$CHANGES" -ne "$before" ]; then
			change "reload systemd units" "—" systemctl daemon-reload
			# Restarting the socket stops the filter (the next connection starts
			# it with the new settings) and clears a failed state.
			change "restart cirrusux-dockerfilter.socket" "—" \
				systemctl restart cirrusux-dockerfilter.socket
		fi
	else
		if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
			ok "docker with compose"
		else
			warn "APPS_RUNTIME=docker needs Docker with its compose plugin: see https://docs.docker.com/engine/install/debian/"
		fi
		# Back from Podman: the filter talks to Docker's socket again.
		local dropin=/etc/systemd/system/cirrusux-dockerfilter.service.d/podman.conf
		if [ -f "$ROOT$dropin" ]; then
			change "point the Docker filter back at Docker (remove $dropin; a dated copy stays)" "cirrusux-setup apply apps with APPS_RUNTIME=podman" \
				_apps_drop_podman "$ROOT$dropin"
		fi
	fi

	if id cirrusux-apps >/dev/null 2>&1; then
		ok "user cirrusux-apps"
	else
		change "create system user cirrusux-apps (apps run as it)" "userdel cirrusux-apps" \
			useradd --system --no-create-home --shell /usr/sbin/nologin cirrusux-apps
	fi
	for r in "$dir" "$archive" /var/lib/cirrusux/apps; do
		if [ -d "$ROOT$r" ]; then
			ok "$r"
		else
			change "create $r" "rmdir $r" mkdir -p -m 755 "$ROOT$r"
		fi
	done
	for r in $roots; do
		[ -d "$ROOT$r" ] || warn "media folder $r doesn't exist yet"
	done

	helper_module apps <<EOF2
# Written by cirrusux-setup (apps module).
CONTAINER_RUNTIME=$rt
APPS_DIR=$dir
APPS_ARCHIVE=$archive
APPS_USER=cirrusux-apps
MEDIA_ROOTS=$roots
${APPS_BIND:+APPS_BIND=$APPS_BIND}
EOF2
}

_apps_drop_podman() {
	backup "$1"
	rm -f "$1"
	if [ -z "${NO_SYSTEMD:-}" ]; then
		systemctl daemon-reload
		systemctl restart cirrusux-dockerfilter.socket
	fi
}
