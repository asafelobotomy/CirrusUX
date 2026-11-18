#!/bin/bash
# verify-release.sh vX.Y.Z | --from DIR: check a release the way install.sh
# does, without installing anything. Run it after scripts/sign-release.sh,
# or on any release you want to be sure of.
#
# It checks that SHA256SUMS carries a signature from the release key (the
# RELEASE_KEY line in this checkout's install.sh), that every file SHA256SUMS
# lists is there and matches it, and, for a folder, that nothing in it is
# missing from SHA256SUMS. It needs no keys of its own and no sign-in.
#
#   vX.Y.Z      download that release from GitHub into a scratch folder
#   --from DIR  a release already downloaded (gh release download vX.Y.Z)
#   --sums-only check the signature on SHA256SUMS only, not the files it
#               lists (no bundles downloaded)
#   CIRRUS_REPO     another repository than asafelobotomy/CirrusUX
#   CIRRUS_SIGNERS  another allowed-signers line than install.sh's (tests,
#                   or a fork's own key)
#
# Needs ssh-keygen, sha256sum and, for a version, curl.
set -euo pipefail
REPO=${CIRRUS_REPO:-asafelobotomy/CirrusUX}
SIGN_AS=release@cirrusux SIGN_NS=cirrusux-release
here=$(cd "$(dirname "$0")/.." && pwd)

die() {
	printf 'verify-release.sh: %s\n' "$*" >&2
	exit 1
}
usage="usage: scripts/verify-release.sh vX.Y.Z | --from DIR [--sums-only]"

version="" from="" sums_only=no
while [ $# -gt 0 ]; do
	case "$1" in
	--from) from=${2:-}; [ -n "$from" ] || die "--from needs a folder"; shift 2 ;;
	--sums-only) sums_only=yes; shift ;;
	v[0-9]*) version=$1; shift ;;
	-h | --help) sed -n '2,/^set /{/^set /d;s/^# \{0,1\}//p}' "$0"; exit 0 ;;
	*) die "$usage" ;;
	esac
done
[ -n "$version" ] || [ -n "$from" ] || die "$usage"
[ -z "$version" ] || [ -z "$from" ] || die "pass a version or --from, not both"
[ -z "$version" ] || [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "not a release version: $version"
for tool in ssh-keygen sha256sum; do
	command -v "$tool" >/dev/null || die "needs $tool"
done

signers=${CIRRUS_SIGNERS-$(sed -n "s/^RELEASE_KEY='\(.*\)'\$/\1/p" "$here/install.sh")}
[ -n "$signers" ] || die "install.sh holds no release key (RELEASE_KEY)"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf '%s\n' "$signers" >"$tmp/signers"

if [ -n "$version" ]; then
	command -v curl >/dev/null || die "needs curl to download $version (or pass --from)"
	url=https://github.com/$REPO/releases/download/$version
	get() { curl -fsSL --proto '=https' --tlsv1.2 -o "$dir/$1" "$url/$1"; }
	dir=$tmp/r
	mkdir "$dir"
	get SHA256SUMS || die "couldn't download $url/SHA256SUMS: is $version published?"
	get SHA256SUMS.sig || die "$version isn't signed: $url/SHA256SUMS.sig is missing (run scripts/sign-release.sh $version)"
else
	[ -d "$from" ] || die "no folder $from"
	dir=$(cd "$from" && pwd)
	[ -f "$dir/SHA256SUMS" ] || die "$from has no SHA256SUMS"
	[ -s "$dir/SHA256SUMS.sig" ] || die "$from isn't signed: no SHA256SUMS.sig"
fi

# The same check install.sh makes before it unpacks anything.
if ! out=$(ssh-keygen -Y verify -f "$tmp/signers" -I "$SIGN_AS" -n "$SIGN_NS" -s "$dir/SHA256SUMS.sig" <"$dir/SHA256SUMS" 2>&1); then
	printf '%s\n' "$out" >&2
	die "SHA256SUMS isn't signed with the release key in install.sh: install.sh would refuse this release"
fi
echo "Signature: SHA256SUMS is signed with the release key ($(ssh-keygen -lf <(grep -o 'ssh-[a-z0-9-]* [A-Za-z0-9+/=]*' "$tmp/signers" | head -1) | cut -d' ' -f2))."

[ "$sums_only" = no ] || exit 0

listed=$(sed -n 's/^[0-9a-f]\{64\} [ *]\(.*\)$/\1/p' "$dir/SHA256SUMS")
[ -n "$listed" ] || die "SHA256SUMS lists no files"
if [ -n "$version" ]; then
	while IFS= read -r f; do
		case "$f" in */* | .*) die "SHA256SUMS lists an odd name: $f" ;; esac
		get "$f" || die "couldn't download $url/$f"
	done <<<"$listed"
else
	# Every file the folder offers is in SHA256SUMS, so nothing rides along unchecked.
	for f in "$dir"/*; do
		case "${f##*/}" in SHA256SUMS | SHA256SUMS.sig) continue ;; esac
		grep -qxF "${f##*/}" <<<"$listed" || die "${f##*/} is in the release but not in SHA256SUMS"
	done
fi
(cd "$dir" && sha256sum -c --quiet --strict SHA256SUMS) || die "a file doesn't match SHA256SUMS"
echo "Checksums: all $(wc -l <<<"$listed") files match SHA256SUMS."

# A release whose own install.sh carries another key than this checkout's
# is a key change: worth knowing before machines upgrade to it.
if [ -f "$dir/install.sh" ]; then
	theirs=$(sed -n "s/^RELEASE_KEY='\(.*\)'\$/\1/p" "$dir/install.sh")
	[ "$theirs" = "$signers" ] ||
		echo "Note: this release's install.sh has another RELEASE_KEY than this checkout's. Machines that install it will trust that key from then on."
fi
echo "OK: ${version:-the release in $from} would install."
