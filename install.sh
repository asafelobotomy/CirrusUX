#!/bin/bash
# install.sh — downloads a CirrusUX release, checks it, and unpacks it to
# /opt/cirrusux, then starts `cirrusux-setup init` to set the machine up.
# Read it before you run it:
#
#   curl -fsSLO https://github.com/asafelobotomy/CirrusUX/releases/latest/download/install.sh
#   less install.sh
#   sudo bash install.sh
#
#   install.sh [--version vX.Y.Z] [--from DIR] [--no-init]
#
#   --version   that release instead of the latest
#   --from DIR  a release already downloaded: DIR holds its .tar.gz and
#               SHA256SUMS (no network needed)
#   --no-init   only unpack it; `cirrusux-setup upgrade` uses this
#
# Releases go in /opt/cirrusux/releases/vX.Y.Z, owned by root; `current`
# points at the one in use and `previous` at the one before, for
# `cirrusux-setup rollback`. Older ones are removed. Nothing on the system
# changes until cirrusux-setup applies it, and only when you say so.
set -euo pipefail

REPO=${CIRRUS_REPO:-asafelobotomy/CirrusUX}
PREFIX=${CIRRUS_PREFIX:-/opt/cirrusux}
version="" from="" init=yes

die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}
say() { printf '%s\n' "$*"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--version) version=${2:-}; shift 2 || die "--version needs a version" ;;
	--from) from=${2:-}; shift 2 || die "--from needs a folder" ;;
	--no-init) init=no; shift ;;
	-h | --help) sed -n '2,/^set /{/^set /d;s/^# \{0,1\}//p}' "$0"; exit 0 ;;
	*) die "unknown option $1 (see --help)" ;;
	esac
done

[ "$(id -u)" = 0 ] || die "run as root: sudo bash install.sh"
[ "$(uname -s)" = Linux ] || die "CirrusUX runs on Linux"
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "no release for this CPU ($(uname -m)): build from source, see docs/INSTALL.md" ;;
esac
{ command -v sha256sum && command -v tar; } >/dev/null || die "needs sha256sum and tar (coreutils, tar)"

# fetch URL FILE
fetch() {
	if command -v curl >/dev/null; then
		curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
	elif command -v wget >/dev/null; then
		wget -q --https-only -O "$2" "$1"
	else
		die "needs curl or wget: sudo apt-get install curl"
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if [ -z "$version" ] && [ -n "$from" ]; then
	# The one bundle for this CPU in the folder names the version.
	f=$(find "$from" -maxdepth 1 -name "cirrusux-v*-linux-$arch.tar.gz" -printf '%f\n' | sort -V | tail -1)
	[ -n "$f" ] || die "no cirrusux-v*-linux-$arch.tar.gz in $from"
	version=${f#cirrusux-}
	version=${version%-linux-"$arch".tar.gz}
fi
if [ -z "$version" ]; then
	fetch "https://api.github.com/repos/$REPO/releases/latest" "$tmp/latest.json" ||
		die "couldn't ask GitHub for the latest release (pass --version, or --from a downloaded one)"
	version=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -1)
fi
[[ $version == v* ]] || version=v$version
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "not a release version: $version"

name=cirrusux-$version-linux-$arch
dest=$PREFIX/releases/$version

if [ -f "$dest/VERSION" ] && [ -x "$dest/deploy/host/cirrusux-setup" ]; then
	say "CirrusUX $version is already unpacked in $dest."
else
	if [ -n "$from" ]; then
		cp "$from/$name.tar.gz" "$from/SHA256SUMS" "$tmp/" 2>/dev/null || die "$from needs $name.tar.gz and SHA256SUMS"
	else
		url=https://github.com/$REPO/releases/download/$version
		say "Downloading CirrusUX $version for $arch…"
		fetch "$url/$name.tar.gz" "$tmp/$name.tar.gz" || die "couldn't download $url/$name.tar.gz"
		fetch "$url/SHA256SUMS" "$tmp/SHA256SUMS" || die "couldn't download $url/SHA256SUMS"
	fi
	grep -q "  $name.tar.gz\$" "$tmp/SHA256SUMS" || die "SHA256SUMS has no line for $name.tar.gz"
	(cd "$tmp" && grep "  $name.tar.gz\$" SHA256SUMS | sha256sum -c --quiet -) ||
		die "$name.tar.gz doesn't match its checksum: not installed"
	say "Checksum matches."

	mkdir -p "$tmp/x"
	tar -xzf "$tmp/$name.tar.gz" -C "$tmp/x" --no-same-owner --no-same-permissions
	[ "v$(cat "$tmp/x/$name/VERSION" 2>/dev/null)" = "$version" ] || die "the bundle doesn't hold $version"
	chmod -R go-w "$tmp/x/$name"
	install -d -m 755 "$PREFIX" "$PREFIX/releases"
	rm -rf "$dest.new"
	mv "$tmp/x/$name" "$dest.new"
	mv -T "$dest.new" "$dest"
	say "Unpacked to $dest."
fi

# Switch `current`; the one it pointed at becomes `previous`.
old=$(readlink "$PREFIX/current" 2>/dev/null || true)
if [ "$old" != "releases/$version" ]; then
	[ -z "$old" ] || ln -sfn "$old" "$PREFIX/previous"
	ln -sfn "releases/$version" "$PREFIX/current.new"
	mv -T "$PREFIX/current.new" "$PREFIX/current"
fi
keep_prev=$(readlink "$PREFIX/previous" 2>/dev/null || true)
for d in "$PREFIX"/releases/v*; do
	[ -d "$d" ] || continue
	case "releases/${d##*/}" in "releases/$version" | "$keep_prev") ;; *) rm -rf "$d" ;; esac
done

setup=$PREFIX/current/deploy/host/cirrusux-setup
if [ "$init" = no ]; then
	exit 0
fi
if [ -f "${CONFIG:-/etc/cirrusux/host.env}" ]; then
	say "This machine is already set up. To move it to $version: sudo $setup apply"
	exit 0
fi
say ""
exec "$setup" init
