#!/bin/sh
# Fails if any source file is over the line limit (400 by default; set
# MAX_LINES to try another). Long files hide bugs and make review hard:
# split them by what they do. Counts every line, comments and blank ones
# included. Generated and vendored files (lock files, the catalogue's JSON,
# icons) aren't source and aren't counted.
set -eu
cd "$(dirname "$0")/.."
max=${MAX_LINES:-400}

over=$(git ls-files -- '*.go' '*.ts' '*.js' '*.svelte' '*.css' '*.sh' \
	'deploy/host/cirrusux-setup' 'deploy/host/files/*' 'deploy/gateway/cirrusux-router-check' |
	while read -r f; do
		[ -f "$f" ] || continue
		n=$(wc -l <"$f")
		[ "$n" -gt "$max" ] && echo "  $f: $n lines"
		true
	done)

if [ -n "$over" ]; then
	echo "Files over $max lines (split them by what they do):"
	echo "$over"
	exit 1
fi
echo "file length: every source file is $max lines or fewer"
