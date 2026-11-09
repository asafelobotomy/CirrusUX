#!/bin/bash
# bundle.sh VERSION ARCH OUT: make OUT/cirrusux-vVERSION-linux-ARCH.tar.gz,
# a release that installs as it is: bin/, deploy/ (without tests or demo
# data), install.sh, docs and the licence, the layout cirrusux-setup expects.
# Files are owned by root in the archive and writable by root only, since
# root runs them (cirrusux-setup refuses a folder anyone else can change).
#
# Run from the checkout after `make web`; it builds the Go binaries itself.
set -euo pipefail
[ $# = 3 ] || { echo "usage: scripts/bundle.sh VERSION ARCH OUT" >&2; exit 64; }
version=$1 arch=$2
mkdir -p "$3"
out=$(cd "$3" && pwd)
case "$arch" in amd64 | arm64) ;; *) echo "ARCH must be amd64 or arm64" >&2; exit 64 ;; esac
cd "$(dirname "$0")/.."

name=cirrusux-v$version-linux-$arch
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
dir=$stage/$name
mkdir -p "$dir/bin" "$dir/docs"

GOOS=linux GOARCH="$arch" make --no-print-directory build-go BIN="$dir/bin" >/dev/null
rm -f "$dir/bin/cirrusstore" # for catalogue publishers, not servers

cp -r deploy "$dir/"
rm -rf "$dir/deploy/host/tests" "$dir/deploy/dev" "$dir/deploy/profiles"
cp install.sh README.md LICENSE CHANGELOG.md VERSION "$dir/"
cp docs/INSTALL.md docs/CONFIG.md docs/APPS.md docs/GIT.md "$dir/docs/"

chmod -R u+rwX,go+rX,go-w "$dir"
tar -C "$stage" --owner=0 --group=0 --numeric-owner --sort=name \
	-czf "$out/$name.tar.gz" "$name"
echo "$out/$name.tar.gz"
