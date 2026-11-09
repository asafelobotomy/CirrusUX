#!/bin/sh
# Writes a new version everywhere it lives: VERSION, web/package.json (and its
# lock file), and a dated CHANGELOG.md section that takes over the
# Unreleased notes. Doesn't commit or tag; the script prints the next steps.
set -eu

v=${1:?usage: scripts/bump-version.sh X.Y.Z}
if ! printf '%s' "$v" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "not a semantic version (X.Y.Z): $v" >&2
	exit 2
fi
cd "$(dirname "$0")/.."

if grep -q "^## \[$v\]" CHANGELOG.md; then
	echo "CHANGELOG.md already has a $v section" >&2
	exit 1
fi
if git rev-parse -q --verify "refs/tags/v$v" >/dev/null; then
	echo "tag v$v already exists" >&2
	exit 1
fi

printf '%s\n' "$v" > VERSION
(cd web && npm version "$v" --no-git-tag-version --allow-same-version >/dev/null)
today=$(date -u +%Y-%m-%d)
sed -i "s/^## \[Unreleased\]\$/## [Unreleased]\n\n## [$v] - $today/" CHANGELOG.md

echo "Version is now $v. Check CHANGELOG.md, then:"
echo "  make test"
echo "  git commit -am 'Release v$v'"
echo "  git tag -a v$v -m 'CirrusUX $v'"
echo "  git push origin main v$v"
