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
#
#   SSH_AUTH_SOCK   the agent that holds the key (a password manager's, a
#                   hardware key's, or ssh-agent)
#   CIRRUS_REPO     another repository than asafelobotomy/CirrusUX
#   --no-attestation  skip the attestation check (a private repository
#                   keeps none)
#
# Needs gh (signed in), ssh-keygen and sha256sum.
set -euo pipefail
REPO=${CIRRUS_REPO:-asafelobotomy/CirrusUX}
SIGN_AS=release@cirrusux SIGN_NS=cirrusux-release
here=$(cd "$(dirname "$0")/.." && pwd)

die() {
	printf 'sign-release.sh: %s\n' "$*" >&2
	exit 1
}

version="" attest=yes
for a; do
	case "$a" in
	--no-attestation) attest=no ;;
	v[0-9]*) version=$a ;;
	*) die "usage: scripts/sign-release.sh vX.Y.Z [--no-attestation]" ;;
	esac
done
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "usage: scripts/sign-release.sh vX.Y.Z [--no-attestation]"
for tool in gh ssh-keygen sha256sum; do
	command -v "$tool" >/dev/null || die "needs $tool"
done

# The key to sign with is the one install.sh checks against.
signers=$(sed -n "s/^RELEASE_KEY='\(.*\)'\$/\1/p" "$here/install.sh")
[ -n "$signers" ] || die "install.sh holds no release key yet (RELEASE_KEY)"
key=$(printf '%s\n' "$signers" | grep -o 'ssh-[a-z0-9-]* [A-Za-z0-9+/=]*' | head -1)
[ -n "$key" ] || die "RELEASE_KEY in install.sh isn't an allowed-signers line"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf '%s\n' "$key" >"$tmp/release.pub"
printf '%s\n' "$signers" >"$tmp/signers"

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
ssh-keygen -Y sign -U -f "$tmp/release.pub" -n "$SIGN_NS" "$tmp/r/SHA256SUMS" >/dev/null 2>&1 ||
	die "signing failed: is the release key's agent running and unlocked (SSH_AUTH_SOCK=${SSH_AUTH_SOCK:-unset})?"
ssh-keygen -Y verify -f "$tmp/signers" -I "$SIGN_AS" -n "$SIGN_NS" -s "$tmp/r/SHA256SUMS.sig" <"$tmp/r/SHA256SUMS" >/dev/null ||
	die "the new signature doesn't verify against install.sh's key: not uploaded"
gh release upload "$version" "$tmp/r/SHA256SUMS.sig" -R "$REPO" --clobber
echo "Signed $version: SHA256SUMS.sig is on the release."
