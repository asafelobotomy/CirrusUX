# Shared helpers for cirrusux-setup modules. Sourced, not run.
#
# Every module describes the state it wants. In check mode a change is only
# reported; in apply mode it's made, after backing up any file it replaces,
# and logged with its undo (the owner's rule 3). Re-running apply when
# nothing differs changes nothing.
#
# ROOT prefixes every file path, so tests can run against a scratch folder.
# Commands (systemctl, nft, useradd…) are looked up on PATH, so tests can
# put stand-ins first.

ROOT=${ROOT:-}
MODE=${MODE:-check}
STAMP=${STAMP:-$(date +%Y%m%d-%H%M)}
STATE_DIR="$ROOT/var/lib/cirrusux"
SETUP_LOG="$STATE_DIR/setup.log"
CHANGES=0
MODULE=${MODULE:-setup}

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
	printf 'cirrusux-setup: %s\n' "$*" >&2
	exit 1
}

# change DESCRIPTION UNDO COMMAND...: report it (check) or run and log it.
change() {
	local what=$1 undo=$2
	shift 2
	CHANGES=$((CHANGES + 1))
	if [ "$MODE" = check ]; then
		say "  ~ $what"
		return 0
	fi
	say "  + $what"
	"$@"
	mkdir -p "$STATE_DIR"
	printf '%s %s: %s — undo: %s\n' "$(date '+%Y-%m-%d %H:%M %Z')" "$MODULE" "$what" "$undo" >>"$SETUP_LOG"
}

ok() { say "  = $*"; }

# backup FILE: keep a dated copy before it's replaced. Never deletes.
# Programs are the exception (see prev_copy): one previous copy, kept off
# the PATH, so updates don't pile copies up in /usr/local/sbin.
backup() {
	local f=$1
	[ -e "$f" ] || return 0
	if is_program "${f#"$ROOT"}"; then
		mkdir -p "$ROOT$PREV_DIR"
		cp -a "$f" "$ROOT$(prev_copy "${f#"$ROOT"}")"
		return 0
	fi
	[ -e "$f.bak-$STAMP" ] || cp -a "$f" "$f.bak-$STAMP"
}

# Programs setup installs keep only the version before the last update.
PREV_DIR=/var/lib/cirrusux/previous
is_program() { case "$1" in /usr/local/bin/* | /usr/local/sbin/*) return 0 ;; *) return 1 ;; esac; }
prev_copy() { echo "$PREV_DIR/$(basename "$1")"; }

# put_file DEST MODE OWNER:GROUP < content — install content if it differs.
put_file() {
	local dest=$1 mode=$2 owner=$3 tmp
	tmp=$(mktemp)
	cat >"$tmp"
	# The owner counts too: sudo ignores a sudoers file root doesn't own.
	if [ -f "$ROOT$dest" ] && cmp -s "$tmp" "$ROOT$dest" && [ "$(file_mode "$ROOT$dest")" = "$mode" ] &&
		{ [ -n "${NO_CHOWN:-}" ] || [ "$(stat -c '%U:%G' "$ROOT$dest")" = "$owner" ]; }; then
		ok "$dest"
		rm -f "$tmp"
		return 0
	fi
	local undo="restore $dest.bak-$STAMP"
	is_program "$dest" && undo="restore $(prev_copy "$dest")"
	[ -e "$ROOT$dest" ] || undo="rm $dest"
	change "write $dest" "$undo" _install "$tmp" "$ROOT$dest" "$mode" "$owner"
	rm -f "$tmp"
}

_install() {
	local src=$1 dest=$2 mode=$3 owner=$4
	mkdir -p "$(dirname "$dest")"
	backup "$dest"
	install -m "$mode" "$src" "$dest"
	[ -n "${NO_CHOWN:-}" ] || chown "$owner" "$dest"
}

# put_once DEST MODE OWNER:GROUP < content — create only if missing: for
# config files the owner edits afterwards.
put_once() {
	local dest=$1
	if [ -e "$ROOT$dest" ]; then
		ok "$dest (exists, left as is)"
		cat >/dev/null
		return 0
	fi
	put_file "$@"
}

file_mode() { stat -c '%a' "$1"; }

# valid_path PATH: an absolute, clean path of plain characters, as the
# helper requires (cmd/helper/config.go): no "//", "." or ".." parts and no
# trailing "/". The helper refuses its whole config otherwise.
valid_path() {
	[[ $1 =~ ^(/[A-Za-z0-9_@+-][A-Za-z0-9._@+-]*)+$ ]] || return 1
	[[ /$1/ != */./* && /$1/ != */../* ]]
}

# valid_ip ADDRESS: an IPv4 address, or an IPv6 one (checked loosely).
valid_ip() {
	local IFS=. a b c d
	if [[ $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		read -r a b c d <<<"$1"
		[ "$a" -le 255 ] && [ "$b" -le 255 ] && [ "$c" -le 255 ] && [ "$d" -le 255 ]
		return
	fi
	[[ $1 =~ ^[0-9a-fA-F:]+$ && $1 == *:*:* ]]
}

# fill_values KEY=VALUE... < example: the example with each KEY's line set
# to VALUE. Done in the shell itself, so values (a token) never appear on a
# command line, where any user could read them in the process list.
fill_values() {
	local line kv
	while IFS= read -r line || [ -n "$line" ]; do
		for kv; do
			if [[ $line == "${kv%%=*}="* ]]; then
				line=$kv
				break
			fi
		done
		printf '%s\n' "$line"
	done
}

# placeholders FILE: lists values still to fill in, e.g. "<set in env>".
placeholders() { { grep -nE '^[A-Z_]+=.*<[^>]+>' "$1" 2>/dev/null || true; } | cut -d= -f1 | cut -d: -f2 | tr '\n' ' '; }

# init_system prints systemd or runit. CIRRUS_INIT chooses; otherwise it's
# systemd when systemd is running, else runit (a gateway machine without
# systemd, e.g. under tini in a container).
init_system() {
	case "${CIRRUS_INIT:-}" in
	systemd | runit) echo "$CIRRUS_INIT" ;;
	"") if [ -d /run/systemd/system ]; then echo systemd; else echo runit; fi ;;
	*) die "CIRRUS_INIT must be systemd or runit" ;;
	esac
}
is_runit() { [ "$(init_system)" = runit ]; }

# Under runit a unit NAME.service is the service folder $SV_DIR/NAME, and
# it runs while linked into runsvdir's folder ($SVDIR, /etc/service).
SV_DIR=/etc/sv
SV_LINKS=${SVDIR:-/etc/service}
sv_name() { echo "${1%.service}"; }

# unit_enabled UNIT: whether it starts at boot.
unit_enabled() {
	if is_runit; then
		[ -L "$ROOT$SV_LINKS/$(sv_name "$1")" ]
		return
	fi
	systemctl is-enabled --quiet "$1" 2>/dev/null
}

# enable_unit UNIT: start it now and at boot.
enable_unit() {
	if is_runit; then
		mkdir -p "$ROOT$SV_LINKS"
		ln -sfn "$SV_DIR/$(sv_name "$1")" "$ROOT$SV_LINKS/$(sv_name "$1")"
		return
	fi
	systemctl enable --now "$1"
}

# disable_how UNIT: the command that undoes enable_unit, for the log.
disable_how() {
	if is_runit; then echo "rm $SV_LINKS/$(sv_name "$1")"; else echo "systemctl disable --now $1"; fi
}

# restart_unit UNIT
restart_unit() {
	if is_runit; then
		sv restart "$ROOT$SV_LINKS/$(sv_name "$1")"
		return
	fi
	systemctl restart "$1"
}

# release_dir: the folder holding CirrusUX's bin/ and deploy/
# (CIRRUS_RELEASE, or this checkout).
release_dir() { echo "${CIRRUS_RELEASE:-$(cd "$HERE/../.." && pwd)}"; }

# short_host: this machine's short host name.
short_host() { hostname -s 2>/dev/null || echo server; }

# start_unit UNIT [WHAT]: enable and start UNIT unless it already is. WHAT
# names it in the report (default: the unit's own name).
start_unit() {
	local what=${2:-$1}
	if unit_enabled "$1"; then
		ok "$what on"
	else
		change "start $what" "$(disable_how "$1")" enable_unit "$1"
	fi
}
unit_active() {
	if is_runit; then
		sv status "$ROOT$SV_LINKS/$(sv_name "$1")" 2>/dev/null | grep -q '^run:'
		return
	fi
	systemctl is-active --quiet "$1" 2>/dev/null
}

# load_config FILE: source the host settings, refusing a file that anyone
# but root could have changed.
load_config() {
	local f=$1
	[ -f "$f" ] || return 0
	if [ -z "${NO_CHOWN:-}" ]; then
		[ "$(stat -c %u "$f")" = 0 ] || die "$f must belong to root"
		[ -z "$(find "$f" -perm /022)" ] || die "$f must be writable only by root"
	fi
	# shellcheck disable=SC1090 # the path is the config file
	. "$f"
}

# need_packages PKG...: install the Debian packages a module needs. Only
# missing ones; nothing is ever upgraded. The package lists are refreshed
# once per run, before the first install: a fresh machine (a cloud image,
# for one) may have none at all.
APT_REFRESHED=no
need_packages() {
	local p missing=()
	for p in "$@"; do
		dpkg-query -W -f '${Status}' "$p" 2>/dev/null | grep -q 'install ok installed' || missing+=("$p")
	done
	if [ ${#missing[@]} -eq 0 ]; then
		ok "packages: $*"
		return 0
	fi
	if [ "$APT_REFRESHED" = no ]; then
		change "refresh the package lists" "—" apt-get update -qq
		APT_REFRESHED=yes
	fi
	change "install packages: ${missing[*]}" "apt-get remove ${missing[*]}" \
		env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${missing[@]}"
}

# helper_module NAME < settings — the file a setup module writes so the
# panel's helper knows about it (/etc/cirrusux/helper.d/NAME.env).
helper_module() { put_file "/etc/cirrusux/helper.d/$1.env" 644 root:root; }

# put_link LINK TARGET — a symlink, replaced if it points elsewhere.
# schedule_rollback UNIT SECS PIDFILE ARGS...: in SECS seconds, run
# `cirrusux-setup ARGS...`, unless cancel_rollback runs first. For changes
# that could lock people out (the firewall, SSH): undone unless confirmed.
schedule_rollback() {
	local unit=$1 secs=$2 pidfile=$3 self
	shift 3
	self=$(readlink -f "$0")
	if command -v systemd-run >/dev/null && [ -z "${NO_SYSTEMD:-}" ]; then
		systemctl stop "$unit.timer" 2>/dev/null || true
		systemctl reset-failed "$unit.timer" "$unit.service" 2>/dev/null || true
		systemd-run --quiet --unit="$unit" --on-active="$secs" "$self" "$@"
	else
		# Without systemd (tests, containers): a detached sleeper.
		(
			sleep "$secs"
			ROOT=$ROOT NO_CHOWN=${NO_CHOWN:-} CONFIG=$CONFIG "$self" "$@"
		) >/dev/null 2>&1 &
		echo $! >"$pidfile"
	fi
}

# cancel_rollback UNIT PIDFILE
cancel_rollback() {
	if command -v systemd-run >/dev/null && [ -z "${NO_SYSTEMD:-}" ]; then
		systemctl stop "$1.timer" 2>/dev/null || true
	elif [ -f "$2" ]; then
		pkill -P "$(cat "$2")" sleep 2>/dev/null || true
		kill "$(cat "$2")" 2>/dev/null || true
		rm -f "$2"
	fi
}

put_link() {
	local link=$1 target=$2
	if [ "$(readlink "$ROOT$link" 2>/dev/null)" = "$target" ]; then
		ok "$link → $target"
		return 0
	fi
	change "link $link → $target" "rm $link" _link "$target" "$ROOT$link"
}

_link() {
	mkdir -p "$(dirname "$2")"
	[ -e "$2" ] && [ ! -L "$2" ] && backup "$2"
	ln -sfn "$1" "$2"
}
