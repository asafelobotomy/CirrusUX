# shellcheck shell=bash
# CirrusStore: which online catalogues this server trusts for apps.
#
# Settings (host.env):
#   STORE_OFFICIAL=yes   trust the official CirrusStore catalogue, whose key
#                        is built into CirrusUX; no leaves only the built-in
#                        apps and the sources below
#   STORE_SOURCES=""     third-party sources, space-separated, each
#                        name|https://host/path/|KEY
#                        where KEY is the last field of the publisher's
#                        NAME.pub ("cirrusstore-key v1 NAME KEY"); compare
#                        its fingerprint in the panel with the publisher's
#
# Each source becomes /etc/cirrusux/store.d/<name>.source (root, mode 644).
# The root helper trusts only these files and the built-in key, and checks
# every app's signature itself before installing it, so the panel can list
# sources but never add one. A source dropped from STORE_SOURCES is removed
# (a dated copy stays). Nothing is fetched here: the panel fetches when the
# owner presses Check CirrusStore.

STORE_DIR=/etc/cirrusux/store.d

module_store() {
	local official=${STORE_OFFICIAL:-yes}
	case "$official" in yes | no) ;; *) die "STORE_OFFICIAL must be yes or no" ;; esac

	local entry name url key want=" "
	for entry in ${STORE_SOURCES:-}; do
		IFS='|' read -r name url key <<<"$entry"
		[[ $name =~ ^[a-z][a-z0-9-]{1,30}$ ]] || die "STORE_SOURCES: source name \"$name\": lowercase letters, digits and -"
		[[ $name != cirrusstore && $name != built-in ]] || die "STORE_SOURCES: \"$name\" is reserved for CirrusUX's own catalogue"
		[[ $want != *" $name "* ]] || die "STORE_SOURCES: $name is listed twice"
		[[ $url =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._~-]+)*/?$ ]] || die "STORE_SOURCES: $name: the URL must be https://host/path/, got \"$url\""
		[[ $key =~ ^[A-Za-z0-9+/]{43}=$ ]] || die "STORE_SOURCES: $name: the key is the base64 field of NAME.pub (44 characters ending in =)"
		[[ $url == */ ]] || url="$url/"
		put_file "$STORE_DIR/$name.source" 644 root:root <<EOF
# Written by cirrusux-setup (store module) from STORE_SOURCES.
url=$url
key=cirrusstore-key v1 $name $key
EOF
		want="$want$name "
	done

	local f
	for f in "$ROOT$STORE_DIR"/*.source; do
		[ -e "$f" ] || continue
		name=$(basename "$f" .source)
		[[ $want == *" $name "* ]] && continue
		change "remove the source $name ($STORE_DIR/$name.source; a dated copy stays)" "restore $STORE_DIR/$name.source.bak-$STAMP" _store_remove "$f"
	done

	helper_module store <<EOF
# Written by cirrusux-setup (store module).
STORE_OFFICIAL=$official
EOF
}

_store_remove() {
	backup "$1"
	rm -f "$1"
}
