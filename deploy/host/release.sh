# shellcheck shell=bash
# Upgrade and rollback, for a machine installed from a release bundle
# (install.sh): /opt/cirrusux/releases/vX.Y.Z, with `current` and `previous`
# links. Sourced by cirrusux-setup.
#
#   upgrade [VERSION] [-y]   download and check a release, switch to it, apply
#   rollback [-y]            switch back to `previous`, apply
#
# Both show what apply would change and ask first (-y: don't ask). Only
# upgrade goes online, and only when you run it.

# _rel_prefix: the folder holding releases/ and the links, or nothing when
# this copy wasn't installed from a bundle.
_rel_prefix() {
	local rel
	rel=$(release_dir)
	case "$(basename "$(dirname "$rel")")" in releases) ;; *) return 0 ;; esac
	[ -L "$(dirname "$(dirname "$rel")")/current" ] && dirname "$(dirname "$rel")"
}

_rel_need_prefix() {
	PREFIX=$(_rel_prefix)
	[ -n "$PREFIX" ] && return 0
	die "this copy ($(release_dir)) wasn't installed from a release: update the checkout (git pull && make build), then run cirrusux-setup apply"
}

# _rel_switch_apply NEWDIR YES: show the check from the new release's own
# setup, ask, and apply. NEWDIR's setup runs, so its modules do the work.
_rel_switch_apply() {
	local new=$1 yes=$2 rc=0
	[ -z "${CIRRUS_RELEASE:-}" ] || warn "CIRRUS_RELEASE is set in host.env, so apply installs from $CIRRUS_RELEASE, not $new: remove that line"
	"$new/deploy/host/cirrusux-setup" check || rc=$?
	[ "$rc" = 0 ] && { say "Nothing to change."; return 0; }
	[ "$rc" = 2 ] || die "the check failed; nothing applied"
	if [ "$yes" != yes ]; then
		[ -t 0 ] || die "not applied: run it again with -y, or from a terminal"
		local a
		read -r -p "Apply these changes? [y/N] " a
		case "$a" in y | Y | yes) ;; *) say "Not applied. Apply later with: sudo cirrusux-setup apply"; return 0 ;; esac
	fi
	"$new/deploy/host/cirrusux-setup" apply
}

setup_upgrade() {
	local version="" yes=no a
	for a; do
		case "$a" in
		-y) yes=yes ;;
		v[0-9]* | [0-9]*) version=$a ;;
		*) die "upgrade [VERSION] [-y]" ;;
		esac
	done
	_rel_need_prefix
	local was args=(--no-init)
	was=$(readlink "$PREFIX/current")
	[ -z "$version" ] || args+=(--version "$version")
	[ -z "${CIRRUS_UPGRADE_FROM:-}" ] || args+=(--from "$CIRRUS_UPGRADE_FROM")
	CIRRUS_PREFIX=$PREFIX bash "$PREFIX/current/install.sh" "${args[@]}"
	local now
	now=$(readlink "$PREFIX/current")
	if [ "$now" = "$was" ]; then
		say "Already on ${now#releases/}."
	else
		say "Switched from ${was#releases/} to ${now#releases/}. Undo: sudo cirrusux-setup rollback"
		log_line "upgrade: ${was#releases/} → ${now#releases/} — undo: cirrusux-setup rollback"
	fi
	_rel_switch_apply "$PREFIX/$now" "$yes"
}

setup_rollback() {
	local yes=no a
	for a; do
		case "$a" in -y) yes=yes ;; *) die "rollback [-y]" ;; esac
	done
	_rel_need_prefix
	local cur prev
	cur=$(readlink "$PREFIX/current")
	prev=$(readlink "$PREFIX/previous" 2>/dev/null || true)
	if [ -z "$prev" ] || [ ! -d "$PREFIX/$prev" ]; then
		die "there's no earlier release to go back to"
	fi
	ln -sfn "$cur" "$PREFIX/previous"
	ln -sfn "$prev" "$PREFIX/current.new"
	mv -T "$PREFIX/current.new" "$PREFIX/current"
	say "Switched from ${cur#releases/} back to ${prev#releases/}. Undo: sudo cirrusux-setup rollback"
	log_line "rollback: ${cur#releases/} → ${prev#releases/} — undo: cirrusux-setup rollback"
	_rel_switch_apply "$PREFIX/$prev" "$yes"
}
