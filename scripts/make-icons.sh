#!/bin/sh
# Renders the app icons in web/public from the SVG sources in web/icons.
# Needs rsvg-convert (librsvg). Run after changing an icon; the PNGs are
# committed so builds don't need it.
set -eu
cd "$(dirname "$0")/../web"
cp icons/icon.svg public/favicon.svg
for size in 192 512; do
	rsvg-convert -w "$size" -h "$size" icons/icon.svg -o "public/icon-$size.png"
done
rsvg-convert -w 512 -h 512 icons/icon-maskable.svg -o public/icon-maskable-512.png
# iOS ignores the manifest icons and doesn't round transparent corners well.
rsvg-convert -w 180 -h 180 icons/icon-maskable.svg -o public/apple-touch-icon.png
ls -l public/*.png
