# shellcheck shell=bash
# `cirrusux-setup init`: a few questions, answered with what this machine
# already says where it can (time zone, Tailscale login, wired card), then
# host.env is written and, once you agree, applied. Sourced by
# cirrusux-setup. Answers are read from standard input, so it can be scripted:
#   printf '1\n\n\n...' | cirrusux-setup init -y
#
# Every setting it writes is in host.env.example and CONFIG.md; edit
# host.env afterwards and apply again to change one.

# _ask VAR QUESTION [DEFAULT]: read one answer; Enter takes the default.
_ask() {
	local __v=$1 __q=$2 __def=${3:-} __a=""
	if [ -n "$__def" ]; then __q="$__q [$__def]"; fi
	printf '%s ' "$__q" >&2
	IFS= read -r __a || true
	[ -t 0 ] || printf '%s\n' "$__a" >&2
	__a=${__a:-$__def}
	printf -v "$__v" '%s' "$__a"
}

# _yes QUESTION DEFAULT(y|n): true for yes.
_yes() {
	local __yn __hint=y/N
	[ "$2" = y ] && __hint=Y/n
	_ask __yn "$1 ($__hint)"
	__yn=${__yn:-$2}
	case "$__yn" in y | Y | yes | Yes) return 0 ;; *) return 1 ;; esac
}

_detect_tz() {
	local tz=""
	command -v timedatectl >/dev/null && tz=$(timedatectl show -p Timezone --value 2>/dev/null || true)
	[ -n "$tz" ] || tz=$(cat "$ROOT/etc/timezone" 2>/dev/null || true)
	[ -n "$tz" ] || { tz=$(readlink "$ROOT/etc/localtime" 2>/dev/null || true); tz=${tz#*zoneinfo/}; }
	echo "${tz:-UTC}"
}

# _detect_iface: the interface of the IPv4 default route.
_detect_iface() {
	awk '$2 == "00000000" { print $1; exit }' "${PROC_NET_ROUTE:-/proc/net/route}" 2>/dev/null || true
}

setup_init() {
	local yes=no a
	for a; do
		case "$a" in -y) yes=yes ;; *) die "init [-y]" ;; esac
	done
	if [ -z "${NO_CHOWN:-}" ] && [ "$(id -u)" -ne 0 ]; then
		die "run as root (sudo cirrusux-setup init)"
	fi
	say "Setting up CirrusUX on $(short_host). Press Enter to take the answer in [brackets]."
	if [ -f "$CONFIG" ]; then
		say "$CONFIG already exists."
		_yes "Replace it? The old one is kept as $(basename "$CONFIG").bak-$STAMP" n || { say "Left as it is."; return 0; }
	fi

	local role
	say ""
	say "How will CirrusUX run here?"
	say "  1) single: everything on this machine, which stays on (the simplest)"
	say "  2) server: this machine sleeps when idle; the panel runs on another, always-on machine"
	say "  3) gateway: that always-on machine, for a split setup"
	local def=1
	is_runit && def=3
	_ask a "Choose 1, 2 or 3:" "$def"
	case "$a" in 1 | single) role=single ;; 2 | server) role=server ;; 3 | gateway) role=gateway ;; *) die "choose 1, 2 or 3" ;; esac
	if is_runit && [ "$role" != gateway ]; then
		die "without systemd only the gateway side runs (3)"
	fi

	local tz login="" server_name="" serve=no iface="" age="" apps=no security=no
	_ask tz "Time zone:" "$(_detect_tz)"
	if ! [[ $tz =~ ^[A-Za-z0-9_+/-]+$ ]] || [ ! -e "$ROOT/usr/share/zoneinfo/$tz" ]; then
		die "not a time zone: $tz"
	fi

	if [ "$role" != server ]; then
		_ask login "Who may sign in to the panel (Tailscale logins, space-separated):" "$(ts_login)"
		[ -n "$login" ] || die "someone has to be allowed in"
		[[ $login =~ ^[A-Za-z0-9._@+\ -]+$ ]] || die "logins have letters, digits and . _ @ + - only"
		if command -v tailscale >/dev/null; then
			_yes "Publish the panel on your tailnet with tailscale serve?" y && serve=yes
		else
			warn "Tailscale isn't installed, so the panel isn't published: see docs/INSTALL.md, \"Sign-in\""
		fi
	fi
	if [ "$role" = gateway ]; then
		_ask server_name "The server's Tailscale name:" "homeserver"
		[[ $server_name =~ ^[A-Za-z0-9.-]+$ ]] || die "not a machine name: $server_name"
	fi
	if [ "$role" != gateway ]; then
		_yes "Install apps from CirrusStore (Podman)?" y && apps=yes
		_yes "Add fail2ban for SSH and a weekly virus scan?" y && security=yes
		_ask age "Back up the system: an age public key (age1…), made on another machine with age-keygen; Enter skips:"
		[ -z "$age" ] || [[ $age =~ ^age1[0-9a-z]{58}$ ]] || die "not an age public key: $age"
	fi
	if [ "$role" = server ]; then
		_ask iface "The wired network card the server is woken on:" "$(_detect_iface)"
		[ -z "$iface" ] || { [[ $iface =~ ^[A-Za-z0-9._-]+$ ]] && [ -e "$ROOT/sys/class/net/$iface" ]; } ||
			die "no network card named $iface (ip link lists them)"
	fi

	local mods="base cirrusux network"
	[ -z "$age" ] || mods="$mods backup"
	[ "$security" = no ] || mods="$mods security"
	[ "$apps" = no ] || mods="$mods apps store" # store: the official catalogue
	[ "$role" != server ] || mods="$mods sleep"

	local tmp
	tmp=$(mktemp)
	{
		echo "# /etc/cirrusux/host.env, written by cirrusux-setup init on $(date +%F)."
		echo "# Every setting is in host.env.example and docs/CONFIG.md. After a change:"
		echo "# sudo cirrusux-setup check, then apply. Add firewall to HOST_MODULES when"
		echo "# everything works (docs/INSTALL.md, \"The firewall\")."
		echo "HOST_MODULES=\"$mods\""
		echo "HOST_TZ=$tz"
		echo "CIRRUS_ROLE=$role"
		[ -z "$login" ] || echo "CIRRUS_ALLOWED_USERS=\"$login\""
		[ -z "$server_name" ] || echo "CIRRUS_SERVER_NAME=$server_name"
		[ "$serve" = no ] || echo "CIRRUS_TAILSCALE_SERVE=yes"
		[ -z "$iface" ] || echo "NET_WOL_IFACE=$iface"
		[ -z "$age" ] || echo "BACKUP_AGE_RECIPIENT=$age"
	} >"$tmp"
	mkdir -p "$(dirname "$CONFIG")"
	[ ! -e "$CONFIG" ] || cp -a "$CONFIG" "$CONFIG.bak-$STAMP"
	install -m 600 "$tmp" "$CONFIG"
	[ -n "${NO_CHOWN:-}" ] || chown root:root "$CONFIG"
	rm -f "$tmp"
	MODULE=init log_line "init: wrote $CONFIG ($role: $mods) — undo: restore $CONFIG.bak-$STAMP or remove it"
	say ""
	say "Wrote $CONFIG:"
	sed -n '/^[A-Z]/s/^/  /p' "$CONFIG"
	say ""

	local rc=0
	"$HERE/cirrusux-setup" check || rc=$?
	[ "$rc" = 0 ] || [ "$rc" = 2 ] || die "the check failed; nothing applied"
	if [ "$rc" = 2 ]; then
		if [ "$yes" = yes ] || _yes "Apply these changes now?" y; then
			"$HERE/cirrusux-setup" apply
		else
			say "Not applied. When ready: sudo cirrusux-setup apply"
			return 0
		fi
	fi
	_init_next "$role" "$serve"
}

_init_next() {
	local dns
	say ""
	case "$1" in
	single)
		dns=$(ts_self_dns)
		if [ "$2" = yes ] && [ -n "$dns" ]; then
			say "Done. Open https://$dns/ from a device on your tailnet."
		else
			say "Done. Put a front end that signs people in before http://127.0.0.1:8064 (docs/INSTALL.md, \"Sign-in\")."
		fi
		;;
	gateway)
		say "Next, connect the server: run  sudo cirrusux-setup pair  here,"
		say "and enter the code it prints into  sudo cirrusux-setup join  on the server."
		;;
	server)
		say "Next, connect the gateway: run  sudo cirrusux-setup pair  on the gateway machine,"
		say "then  sudo cirrusux-setup join  here, and enter its code."
		;;
	esac
}
