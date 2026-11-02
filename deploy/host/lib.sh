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
backup() {
	local f=$1
	[ -e "$f" ] || return 0
	[ -e "$f.bak-$STAMP" ] || cp -a "$f" "$f.bak-$STAMP"
}

# put_file DEST MODE OWNER:GROUP < content — install content if it differs.
put_file() {
	local dest=$1 mode=$2 owner=$3 tmp
	tmp=$(mktemp)
	cat >"$tmp"
	if [ -f "$ROOT$dest" ] && cmp -s "$tmp" "$ROOT$dest" && [ "$(file_mode "$ROOT$dest")" = "$mode" ]; then
		ok "$dest"
		rm -f "$tmp"
		return 0
	fi
	local undo="restore $dest.bak-$STAMP"
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

# placeholders FILE: lists values still to fill in, e.g. "<set in env>".
placeholders() { { grep -nE '^[A-Z_]+=.*<[^>]+>' "$1" 2>/dev/null || true; } | cut -d= -f1 | cut -d: -f2 | tr '\n' ' '; }

# unit_enabled UNIT: whether systemd starts it at boot.
unit_enabled() { systemctl is-enabled --quiet "$1" 2>/dev/null; }
unit_active() { systemctl is-active --quiet "$1" 2>/dev/null; }

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
# missing ones; nothing is ever upgraded.
need_packages() {
	local p missing=()
	for p in "$@"; do
		dpkg-query -W -f '${Status}' "$p" 2>/dev/null | grep -q 'install ok installed' || missing+=("$p")
	done
	if [ ${#missing[@]} -eq 0 ]; then
		ok "packages: $*"
		return 0
	fi
	change "install packages: ${missing[*]}" "apt-get remove ${missing[*]}" \
		env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${missing[@]}"
}

# helper_module NAME < settings — the file a setup module writes so the
# panel's helper knows about it (/etc/cirrusux/helper.d/NAME.env).
helper_module() { put_file "/etc/cirrusux/helper.d/$1.env" 644 root:root; }
