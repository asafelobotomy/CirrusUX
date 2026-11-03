# shellcheck shell=bash
# FTPS (opt-in): pure-ftpd with TLS required, one virtual user for the media.
#
# Settings (host.env):
#   FTPS_USER=media                the one login (a virtual user, not a system one)
#   FTPS_HOME=/mnt/media           where it lands, and all it can see
#   FTPS_PASSIVE_PORTS="39000 40000"   the passive data ports, first and last
#   FTPS_UID, FTPS_GID             the IDs its files get; APPS_MEDIA_UID and
#                                  APPS_MEDIA_GID by default
#
# Plain FTP is refused (TLS=3), anonymous logins are off, and the user is
# kept inside FTPS_HOME. The certificate is self-signed, made once. The
# password is never in a file of ours: set it with
# `sudo cirrusux-setup ftps-passwd`, which asks for it.
#
# The firewall module already lets the LAN and the tailnet reach every port,
# passive ones included. If SLEEP_WATCH_PORTS is set, add 21 so a transfer
# keeps the server awake (the data ports come and go).

FTPS_CONF=/etc/pure-ftpd/conf
FTPS_PDB=/etc/pure-ftpd/pureftpd.pdb
FTPS_PASSWD=/etc/pure-ftpd/pureftpd.passwd
FTPS_CERT=/etc/ssl/private/pure-ftpd.pem

ftps_settings() {
	FTPS_USER=${FTPS_USER:-media}
	FTPS_HOME=${FTPS_HOME:-/mnt/media}
	FTPS_UID=${FTPS_UID:-${APPS_MEDIA_UID:-}}
	FTPS_GID=${FTPS_GID:-${APPS_MEDIA_GID:-}}
	[[ $FTPS_USER =~ ^[a-z_][a-z0-9_-]*$ ]] || die "FTPS_USER must be a plain user name"
	valid_path "$FTPS_HOME" || die "FTPS_HOME must be an absolute path without spaces, special characters, \"..\" or a trailing /"
	[[ $FTPS_UID =~ ^[0-9]+$ && $FTPS_GID =~ ^[0-9]+$ ]] ||
		die "FTPS_UID and FTPS_GID (or APPS_MEDIA_UID and APPS_MEDIA_GID) must be set: the IDs the FTPS user's files get"
}

module_ftps() {
	ftps_settings
	local ports=${FTPS_PASSIVE_PORTS:-39000 40000} lo hi
	read -r lo hi <<<"$ports"
	[[ $lo =~ ^[0-9]+$ && $hi =~ ^[0-9]+$ ]] && [ "$lo" -ge 1024 ] && [ "$lo" -lt "$hi" ] && [ "$hi" -le 65535 ] ||
		die "FTPS_PASSIVE_PORTS must be two port numbers, first and last, above 1023"

	need_packages pure-ftpd openssl
	local before=$CHANGES min_uid=1000
	[ "$FTPS_UID" -ge 1000 ] || min_uid=$FTPS_UID
	# Debian's pure-ftpd reads one setting per file in conf/.
	_ftps_conf TLS 3
	_ftps_conf PassivePortRange "$lo $hi"
	_ftps_conf NoAnonymous yes
	_ftps_conf ChrootEveryone yes
	_ftps_conf UnixAuthentication no
	_ftps_conf PAMAuthentication no
	_ftps_conf PureDB "$FTPS_PDB"
	_ftps_conf MinUID "$min_uid"
	put_link /etc/pure-ftpd/auth/50puredb ../conf/PureDB

	if [ -f "$ROOT$FTPS_CERT" ]; then
		ok "$FTPS_CERT"
	else
		change "make a self-signed certificate ($FTPS_CERT)" "rm $FTPS_CERT" _ftps_cert
	fi
	[ -d "$ROOT$FTPS_HOME" ] || warn "FTPS_HOME $FTPS_HOME doesn't exist yet"

	if ! unit_enabled pure-ftpd.service; then
		change "start pure-ftpd" "systemctl disable --now pure-ftpd" systemctl enable --now pure-ftpd.service
	elif [ "$CHANGES" -ne "$before" ]; then
		change "restart pure-ftpd with its settings" "—" systemctl restart pure-ftpd.service
	else
		ok "pure-ftpd on"
	fi
	if ! grep -q "^$FTPS_USER:" "$ROOT$FTPS_PASSWD" 2>/dev/null; then
		warn "no FTPS user yet: run 'sudo cirrusux-setup ftps-passwd' to choose $FTPS_USER's password"
	fi
}

_ftps_conf() { put_file "$FTPS_CONF/$1" 644 root:root <<<"$2"; }

_ftps_cert() {
	local tmp
	tmp=$(mktemp -d)
	openssl req -x509 -newkey rsa:3072 -nodes -days 3650 -subj "/CN=$(short_host)" \
		-keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
	mkdir -p "$(dirname "$ROOT$FTPS_CERT")"
	install -m 600 /dev/null "$ROOT$FTPS_CERT"
	cat "$tmp/key.pem" "$tmp/cert.pem" >"$ROOT$FTPS_CERT"
	rm -rf "$tmp"
}

# ftps_passwd asks for the FTPS user's password (pure-pw reads it from the
# terminal, twice), creating the user the first time. Nothing stores it but
# pure-ftpd's own hashed database.
ftps_passwd() {
	ftps_settings
	[ -t 0 ] || die "ftps-passwd asks for the password: run it in a terminal"
	command -v pure-pw >/dev/null || die "pure-ftpd isn't installed: apply the ftps module first"
	if grep -q "^$FTPS_USER:" "$ROOT$FTPS_PASSWD" 2>/dev/null; then
		change "set a new password for the FTPS user $FTPS_USER" "set it again with cirrusux-setup ftps-passwd" \
			pure-pw passwd "$FTPS_USER" -f "$ROOT$FTPS_PASSWD" -F "$ROOT$FTPS_PDB" -m
	else
		change "create the FTPS user $FTPS_USER ($FTPS_UID:$FTPS_GID, in $FTPS_HOME)" "pure-pw userdel $FTPS_USER -m" \
			pure-pw useradd "$FTPS_USER" -u "$FTPS_UID" -g "$FTPS_GID" -d "$FTPS_HOME" -f "$ROOT$FTPS_PASSWD" -F "$ROOT$FTPS_PDB" -m
	fi
}
