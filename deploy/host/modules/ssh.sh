# shellcheck shell=bash
# SSH (opt-in): only these users, from these networks, may log in.
#
# Settings (host.env):
#   SSH_ALLOW="alice@192.0.2.0/24 100.64.0.0/10"
#                              who may log in over SSH, and from where:
#                              USER@NETWORK, or a NETWORK alone for anyone
#                              there; written as sshd's AllowUsers
#   SSH_CONFIRM_SECONDS=120    how long a change waits for confirmation
#
# It writes /etc/ssh/sshd_config.d/50-restrict-sources.conf and nothing
# else: password logins, keys and the rest of sshd's settings stay as they
# are. Like the firewall, it refuses a change that would shut out the SSH
# session running it, and a change rolls back by itself unless `cirrusux-setup
# ssh-confirm` runs in time. Without SSH_ALLOW, the file is removed.

SSH_DROPIN=/etc/ssh/sshd_config.d/50-restrict-sources.conf
SSH_DIR=/var/lib/cirrusux/ssh
SSH_TIMER=cirrusux-ssh-rollback

# ssh_dropin prints the drop-in the settings ask for.
ssh_dropin() {
	local w users=()
	for w in ${SSH_ALLOW:-}; do
		[[ $w == *@* ]] || w="*@$w"
		[[ ${w%%@*} =~ ^(\*|[a-z_][a-z0-9_-]*)$ && ${w#*@} =~ ^[0-9a-fA-F.:]+(/[0-9]+)?$ ]] ||
			die "SSH_ALLOW: \"$w\" isn't USER@NETWORK or a NETWORK"
		users+=("$w")
	done
	printf '# Written by cirrusux-setup (ssh module). Edit host.env (SSH_ALLOW), not this file.\n'
	printf 'AllowUsers %s\n' "${users[*]}"
}

module_ssh() {
	# The firewall module's ssh_clients and in_net: who's connected, and
	# whether a network includes them.
	# shellcheck source=modules/firewall.sh
	. "$(dirname "${BASH_SOURCE[0]}")/firewall.sh"
	if [ -z "${SSH_ALLOW:-}" ]; then
		if [ -f "$ROOT$SSH_DROPIN" ]; then
			change "remove $SSH_DROPIN (SSH_ALLOW is empty; a dated copy stays)" "cirrusux-setup apply ssh" _ssh_remove
		else
			ok "no SSH source restriction"
		fi
		return 0
	fi
	command -v sshd >/dev/null || die "sshd isn't installed (apt install openssh-server)"
	[ -n "${NO_SYSTEMD:-}" ] || _ssh_unit >/dev/null ||
		die "neither ssh.service nor sshd.service exists, so sshd can't be reloaded; nothing changed"
	local want
	want=$(mktemp)
	ssh_dropin >"$want"
	if [ -f "$ROOT$SSH_DROPIN" ] && cmp -s "$want" "$ROOT$SSH_DROPIN"; then
		ok "$SSH_DROPIN"
		rm -f "$want"
		return 0
	fi
	ssh_lockout_check
	change "restrict SSH logins ($SSH_ALLOW); rolls back in ${SSH_CONFIRM_SECONDS:-120} s unless confirmed" \
		"cirrusux-setup ssh-rollback" _ssh_try "$want"
	if [ "$MODE" = check ]; then
		sed 's/^/    | /' "$want"
	fi
	rm -f "$want"
}

# ssh_lockout_check refuses a restriction that leaves out an address an SSH
# session comes from right now.
ssh_lockout_check() {
	local client w ok
	for client in $(ssh_clients); do
		ok=""
		for w in $SSH_ALLOW; do
			if in_net "$client" "${w#*@}"; then ok=1; fi
		done
		[ -n "$ok" ] || die "an SSH session comes from $client, which SSH_ALLOW doesn't include: it would be shut out"
	done
}

# _ssh_try FILE: keep the drop-in as it was, put FILE in place, check sshd
# accepts it (or put the old one back), reload sshd and start the rollback.
_ssh_try() {
	local stamp
	stamp=$(date +%Y%m%d-%H%M%S)
	mkdir -p "$ROOT$SSH_DIR" "$(dirname "$ROOT$SSH_DROPIN")"
	if [ -f "$ROOT$SSH_DROPIN" ]; then cp -a "$ROOT$SSH_DROPIN" "$ROOT$SSH_DIR/$stamp-before.conf"; else : >"$ROOT$SSH_DIR/$stamp-before.conf"; fi
	install -m 644 "$1" "$ROOT$SSH_DROPIN"
	if ! sshd -t; then
		_ssh_put_back "$ROOT$SSH_DIR/$stamp-before.conf"
		die "sshd refuses the new settings (sshd -t); nothing changed"
	fi
	schedule_rollback "$SSH_TIMER" "${SSH_CONFIRM_SECONDS:-120}" "$ROOT$SSH_DIR/rollback.pid" ssh-rollback "$SSH_DIR/$stamp-before.conf"
	echo "$stamp" >"$ROOT$SSH_DIR/pending"
	_ssh_reload
	say "    Done. Check you can still log in over SSH (a new session), then within ${SSH_CONFIRM_SECONDS:-120} s run:"
	say "      sudo cirrusux-setup ssh-confirm"
	say "    Otherwise the previous settings come back on their own."
}

# _ssh_unit prints the SSH service's name: ssh.service (Debian) or
# sshd.service (most others).
_ssh_unit() {
	local u
	for u in ssh.service sshd.service; do
		if systemctl list-unit-files "$u" 2>/dev/null | grep -q "^$u"; then
			echo "$u"
			return 0
		fi
	done
	return 1
}

_ssh_reload() { [ -n "${NO_SYSTEMD:-}" ] || systemctl reload "$(_ssh_unit)"; }

_ssh_put_back() {
	if [ -s "$1" ]; then install -m 644 "$1" "$ROOT$SSH_DROPIN"; else rm -f "$ROOT$SSH_DROPIN"; fi
}

_ssh_remove() {
	backup "$ROOT$SSH_DROPIN"
	rm -f "$ROOT$SSH_DROPIN"
	_ssh_reload
}

ssh_confirm() {
	local pending
	pending=$(cat "$ROOT$SSH_DIR/pending" 2>/dev/null) || die "no SSH change is waiting for confirmation"
	cancel_rollback "$SSH_TIMER" "$ROOT$SSH_DIR/rollback.pid"
	change "confirm the SSH restriction from $pending" "cirrusux-setup ssh-rollback $SSH_DIR/$pending-before.conf" \
		rm -f "$ROOT$SSH_DIR/pending"
}

# ssh_rollback [FILE]: back to FILE (empty: no drop-in), by default the one
# from before the last change.
ssh_rollback() {
	local prev=${1:+$ROOT$1}
	if [ -z "$prev" ]; then
		local all=("$ROOT$SSH_DIR"/*-before.conf)
		prev=${all[-1]}
	fi
	[ -f "$prev" ] || die "no earlier SSH settings to go back to"
	change "roll SSH logins back to $(basename "$prev")" "cirrusux-setup apply ssh" _ssh_back "$prev"
	rm -f "$ROOT$SSH_DIR/pending" "$ROOT$SSH_DIR/rollback.pid"
}

_ssh_back() {
	_ssh_put_back "$1"
	_ssh_reload
}
