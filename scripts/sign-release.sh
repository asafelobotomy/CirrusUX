#!/bin/bash
# sign-release.sh vX.Y.Z: sign a published release so install.sh and
# `cirrusux-setup upgrade` accept it. Run it once the release workflow has
# finished; until then the release installs nowhere.
#
# It downloads the release, checks every file against SHA256SUMS and against
# GitHub's build attestation (so what gets signed is what the workflow built
# from this repository), signs SHA256SUMS with the release key and uploads
# the signature as SHA256SUMS.sig.
#
# The private key never touches this machine's disk: an SSH agent holds it
# and does the signing. Which key is the one in install.sh (RELEASE_KEY).
# Before anything is downloaded it checks that the agent offers that key;
# `--check-agent` makes only that check, so you can fix the agent before
# the release is tagged. Afterwards scripts/verify-release.sh checks the
# signature GitHub now serves, as install.sh will.
#
#   SSH_AUTH_SOCK   the agent that holds the key; unset, Proton Pass's:
#                   the pass-cli daemon's ~/.ssh/proton-pass-agent.sock or
#                   the desktop app's ~/.ssh/proton-pass-ssh-agent.sock,
#                   whichever offers the key
#   CIRRUS_REPO     another repository than asafelobotomy/CirrusUX
#   CIRRUS_SIGNERS  another allowed-signers line than install.sh's (tests,
#                   or a fork's own key)
#   --no-attestation  skip the attestation check (a private repository
#                   keeps none)
#   --check-agent   only check that the agent offers the release key
#
# Needs gh (signed in), ssh-add, ssh-keygen, sha256sum and curl.
set -euo pipefail
REPO=${CIRRUS_REPO:-asafelobotomy/CirrusUX}
SIGN_AS=release@cirrusux SIGN_NS=cirrusux-release
here=$(cd "$(dirname "$0")/.." && pwd)

die() {
	printf 'sign-release.sh: %s\n' "$*" >&2
	exit 1
}

usage="usage: scripts/sign-release.sh vX.Y.Z [--no-attestation] | --check-agent"
version="" attest=yes check_only=no
for a; do
	case "$a" in
	--no-attestation) attest=no ;;
	--check-agent) check_only=yes ;;
	v[0-9]*) version=$a ;;
	*) die "$usage" ;;
	esac
done
[ "$check_only" = yes ] || [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "$usage"
for tool in ssh-add ssh-keygen; do
	command -v "$tool" >/dev/null || die "needs $tool (openssh-client)"
done

# The key to sign with is the one install.sh checks against.
signers=${CIRRUS_SIGNERS-$(sed -n "s/^RELEASE_KEY='\(.*\)'\$/\1/p" "$here/install.sh")}
[ -n "$signers" ] || die "install.sh holds no release key yet (RELEASE_KEY)"
key=$(printf '%s\n' "$signers" | grep -o 'ssh-[a-z0-9-]* [A-Za-z0-9+/=]*' | head -1)
[ -n "$key" ] || die "RELEASE_KEY in install.sh isn't an allowed-signers line"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf '%s\n' "$key" >"$tmp/release.pub"
printf '%s\n' "$signers" >"$tmp/signers"
fp=$(ssh-keygen -lf "$tmp/release.pub" | cut -d' ' -f2)

# The agent must offer the release key. Only public keys are listed here.
# Without SSH_AUTH_SOCK, Proton Pass's sockets are tried in turn: the
# pass-cli daemon's, then the desktop app's. The first that offers the key
# is used; if none does, the first that exists is the one reported.
proton_socks=("$HOME/.ssh/proton-pass-agent.sock" "$HOME/.ssh/proton-pass-ssh-agent.sock")
offers_key() { SSH_AUTH_SOCK=$1 ssh-add -L 2>/dev/null | grep -qF "$key"; }
if [ -z "${SSH_AUTH_SOCK:-}" ]; then
	found=""
	for sock in "${proton_socks[@]}"; do
		[ -S "$sock" ] || continue
		[ -n "$found" ] || found=$sock
		if offers_key "$sock"; then
			found=$sock
			break
		fi
	done
	if [ -n "$found" ]; then
		export SSH_AUTH_SOCK=$found
		echo "Agent: using Proton Pass's ($found)."
	fi
fi
[ -n "${SSH_AUTH_SOCK:-}" ] ||
	die "SSH_AUTH_SOCK is unset and no Proton Pass agent socket exists (${proton_socks[*]}): start one (pass-cli ssh-agent daemon start, or turn on the SSH agent in the Proton Pass app) or point SSH_AUTH_SOCK at the agent that holds the release key ($fp)"
offered=$(ssh-add -L 2>/dev/null) || offered=""
if ! grep -qF "$key" <<<"$offered"; then
	{
		echo "sign-release.sh: the agent at $SSH_AUTH_SOCK doesn't offer the release key $fp."
		if [ -n "$offered" ]; then
			echo "It offers:"
			ssh-add -l 2>/dev/null | sed 's/^/  /'
		else
			echo "It offers no keys, or isn't running."
		fi
		case " ${proton_socks[*]} " in
		*" $SSH_AUTH_SOCK "*)
			echo "Proton Pass's pass-cli agent looks for new keys only every hour (--refresh-interval): restart it so it loads the key now:"
			echo "  pass-cli ssh-agent daemon stop && pass-cli ssh-agent daemon start"
			echo "The desktop app's agent needs the app unlocked."
			;;
		esac
		echo "See CONTRIBUTING.md, \"When the agent doesn't offer the key\". Don't export the private key to a file."
	} >&2
	exit 1
fi
echo "Agent: offers the release key $fp."
[ "$check_only" = no ] || exit 0
for tool in gh sha256sum curl; do
	command -v "$tool" >/dev/null || die "needs $tool"
done

gh release download "$version" -R "$REPO" -D "$tmp/r" || die "couldn't download $version from $REPO"
[ -f "$tmp/r/SHA256SUMS" ] || die "$version has no SHA256SUMS: has the release workflow finished?"
(cd "$tmp/r" && sha256sum -c --quiet SHA256SUMS) || die "a file in $version doesn't match SHA256SUMS: not signed"
# Every file the release offers is in SHA256SUMS, so nothing rides along unsigned.
for f in "$tmp"/r/*; do
	case "${f##*/}" in SHA256SUMS | SHA256SUMS.sig) continue ;; esac
	grep -q "  ${f##*/}\$" "$tmp/r/SHA256SUMS" || die "${f##*/} is in the release but not in SHA256SUMS: not signed"
done
if [ "$attest" = yes ]; then
	for f in "$tmp"/r/*.tar.gz "$tmp/r/install.sh"; do
		gh attestation verify "$f" -R "$REPO" >/dev/null || die "${f##*/} has no build attestation from $REPO: not signed"
	done
	echo "Attestations: the release workflow built these files."
fi

rm -f "$tmp/r/SHA256SUMS.sig"
# A password manager's agent may ask you to approve this signature.
if ! out=$(ssh-keygen -Y sign -U -f "$tmp/release.pub" -n "$SIGN_NS" "$tmp/r/SHA256SUMS" 2>&1); then
	printf '%s\n' "$out" >&2
	die "the agent didn't sign: was the request approved, and is the agent unlocked?"
fi
ssh-keygen -Y verify -f "$tmp/signers" -I "$SIGN_AS" -n "$SIGN_NS" -s "$tmp/r/SHA256SUMS.sig" <"$tmp/r/SHA256SUMS" >/dev/null ||
	die "the new signature doesn't verify against install.sh's key: not uploaded"
gh release upload "$version" "$tmp/r/SHA256SUMS.sig" -R "$REPO" --clobber
echo "Signed $version: SHA256SUMS.sig is on the release. Checking what GitHub serves now:"
"$here/scripts/verify-release.sh" "$version" --sums-only ||
	die "the uploaded signature doesn't check out from GitHub: look at the release before anyone upgrades"
