#!/bin/bash
# Run by run.sh inside `unshare -r`: the opt-in pieces (the ssh and ftps
# modules, fail2ban's settings, the sleep module's resume fixes) against a
# scratch ROOT, with stand-ins.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/root/etc/cirrusux $T/rel/bin
cat > $T/stub/systemctl <<'S'
#!/bin/sh
f="$STUBDIR/enabled"; touch "$f"; echo "systemctl $*" >> "$STUBDIR/calls"
case "$1" in
is-enabled|is-active) grep -qx "$3" "$f" ;;
enable) shift; [ "$1" = --now ] && shift; for u; do echo "$u" >> "$f"; done ;;
*) exit 0 ;;
esac
S
printf '#!/bin/sh\nfor p; do :; done\ngrep -qx "$p" "$STUBDIR/installed" 2>/dev/null && printf "install ok installed"\n' > $T/stub/dpkg-query
printf '#!/bin/sh\n[ "$1" = update ] && exit 0\nfor p; do case "$p" in -*|install) ;; *) echo "$p" >> "$STUBDIR/installed" ;; esac; done\n' > $T/stub/apt-get
printf '#!/bin/sh\necho "sshd $*" >> "$STUBDIR/calls"\n[ ! -f "$STUBDIR/sshd-refuses" ]\n' > $T/stub/sshd
printf '#!/bin/sh\necho "sysctl $*" >> "$STUBDIR/calls"\n' > $T/stub/sysctl
# openssl: writes the files it was asked for.
cat > $T/stub/openssl <<'S'
#!/bin/sh
while [ $# -gt 0 ]; do case "$1" in -keyout) echo KEY > "$2" ;; -out) echo CERT > "$2" ;; esac; shift; done
S
printf '#!/bin/sh\necho "pure-pw $*" >> "$STUBDIR/calls"\n' > $T/stub/pure-pw
chmod +x $T/stub/*
touch $T/installed
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 CONFIG=$T/host.env NO_SYSTEMD=1 NO_SESSION_SCAN=1 CIRRUS_INIT=systemd
unset SSH_CONNECTION
fail() { echo "FAIL: $*"; exit 1; }
D=$ROOT/etc/ssh/sshd_config.d/50-restrict-sources.conf

# ssh: written, checked by sshd -t, rolled back unless confirmed.
printf 'SSH_ALLOW="alice@192.168.1.0/24 rm -rf"\n' > $CONFIG
$H/cirrusux-setup check ssh > /dev/null 2>&1 && fail "an SSH_ALLOW that isn't networks accepted"
printf 'SSH_ALLOW="alice@192.168.1.0/24 100.64.0.0/10"\nSSH_CONFIRM_SECONDS=1\n' > $CONFIG
SSH_CONNECTION="10.9.9.9 50000 192.168.1.2 22" $H/cirrusux-setup apply ssh > $T/out 2>&1 && fail "a restriction shutting out this session applied"
grep -q "comes from 10.9.9.9" $T/out || { cat $T/out; fail "wrong refusal"; }
$H/cirrusux-setup apply ssh > $T/out 2>&1 || { cat $T/out; fail "ssh apply"; }
grep -qx "AllowUsers alice@192.168.1.0/24 \*@100.64.0.0/10" $D && grep -q "sshd -t" $T/calls || fail "drop-in or sshd -t"
grep -q PasswordAuthentication $D && fail "password logins touched"
sleep 3
[ ! -e $D ] || fail "not rolled back unconfirmed"
echo "1 ssh: refuses to shut out this session; written, checked; rolled back unconfirmed"

printf 'SSH_ALLOW="alice@192.168.1.0/24"\nSSH_CONFIRM_SECONDS=60\n' > $CONFIG
$H/cirrusux-setup apply ssh > /dev/null 2>&1 || fail "ssh apply 2"
$H/cirrusux-setup ssh-confirm > $T/out 2>&1 || { cat $T/out; fail "ssh-confirm"; }
[ -e $D ] && [ ! -e $ROOT/var/lib/cirrusux/ssh/pending ] || fail "not kept once confirmed"
$H/cirrusux-setup check ssh > /dev/null 2>&1 || fail "re-check"
touch $T/sshd-refuses
printf 'SSH_ALLOW="bob@192.168.1.0/24"\n' > $CONFIG
$H/cirrusux-setup apply ssh > $T/out 2>&1 && fail "sshd -t refused, yet applied"
grep -qx "AllowUsers alice@192.168.1.0/24" $D || fail "the old drop-in wasn't put back"
rm $T/sshd-refuses
echo "2 ssh: confirmed is kept; a file sshd refuses is put back"

# fail2ban's settings.
printf 'SEC_AV=no\nSEC_FAIL2BAN_IGNOREIP="127.0.0.0/8 100.64.0.0/10"\nSEC_FAIL2BAN_FINDTIME=300\n' > $CONFIG
$H/cirrusux-setup apply security > $T/out 2>&1 || { cat $T/out; fail "security apply"; }
J=$ROOT/etc/fail2ban/jail.d/cirrusux.local
grep -qx "ignoreip = 127.0.0.0/8 100.64.0.0/10" $J && grep -qx "bantime = 600" $J && grep -qx "findtime = 300" $J &&
	grep -qx "maxretry = 5" $J && grep -qx "banaction = nftables-multiport" $J || { cat $J; fail "jail settings"; }
printf 'SEC_AV=no\nSEC_FAIL2BAN_MAXRETRY=many\n' > $CONFIG
$H/cirrusux-setup check security > /dev/null 2>&1 && fail "a maxretry that isn't a number accepted"
echo "3 fail2ban: ignoreip, times and retries from host.env; nftables bans"

# The sleep module's resume fixes and wakeup devices.
echo "idle" > $T/rel/bin/cirrusux-idle
W=$ROOT/sys/bus/pci/devices/0000:00:14.0/power
mkdir -p $W && echo enabled > $W/wakeup
printf 'CIRRUS_ROLE=server\nCIRRUS_RELEASE=%s\nNET_WOL_IFACE=enp1s0\nSLEEP_RESUME_FIXES=yes\nSLEEP_NO_WAKEUP_DEVICES="00:14.0"\n' $T/rel > $CONFIG
$H/cirrusux-setup check sleep > /dev/null 2>&1 && fail "a PCI address without its domain accepted"
sed -i 's/"00:14.0"/"0000:00:14.0"/' $CONFIG
$H/cirrusux-setup apply sleep > $T/out 2>&1 || { cat $T/out; fail "sleep apply"; }
R=$ROOT/etc/systemd/system/cirrusux-resume.service
grep -q "ethtool -s enp1s0 wol g" $R && grep -q "restart systemd-timesyncd" $R && grep -q "WantedBy=suspend.target" $R || fail "resume unit"
grep -qx cirrusux-resume.service $T/enabled || fail "resume unit not on"
grep -q 'KERNEL=="0000:00:14.0", ATTR{power/wakeup}="disabled"' $ROOT/etc/udev/rules.d/60-cirrusux-no-wakeup.rules || fail "udev rule"
[ "$(cat $W/wakeup)" = disabled ] || fail "wakeup not switched off now"
$H/cirrusux-setup check sleep > $T/out 2>&1 || { cat $T/out; fail "sleep re-check"; }
echo "4 sleep: resume fixes after each resume; the USB controller may not wake it"

# ftps
printf 'FTPS_HOME=/mnt/media\n' > $CONFIG
$H/cirrusux-setup check ftps > $T/out 2>&1 && fail "ftps without the media IDs accepted"
printf 'APPS_MEDIA_UID=10000\nAPPS_MEDIA_GID=2000\nFTPS_PASSIVE_PORTS="39000 40000"\n' > $CONFIG
$H/cirrusux-setup apply ftps > $T/out 2>&1 || { cat $T/out; fail "ftps apply"; }
C=$ROOT/etc/pure-ftpd/conf
[ "$(cat $C/TLS)" = 3 ] && [ "$(cat $C/PassivePortRange)" = "39000 40000" ] && [ "$(cat $C/NoAnonymous)" = yes ] &&
	[ "$(cat $C/UnixAuthentication)" = no ] || fail "pure-ftpd settings"
[ "$(readlink $ROOT/etc/pure-ftpd/auth/50puredb)" = ../conf/PureDB ] || fail "PureDB not used"
[ "$(stat -c %a $ROOT/etc/ssl/private/pure-ftpd.pem)" = 600 ] && grep -q KEY $ROOT/etc/ssl/private/pure-ftpd.pem || fail "certificate"
grep -qx pure-ftpd.service $T/enabled || fail "pure-ftpd not started"
grep -q "run 'sudo cirrusux-setup ftps-passwd'" $T/out || fail "no word about the password"
grep -rqi pass $ROOT/etc/cirrusux 2>/dev/null && fail "a password-ish thing in /etc/cirrusux"
$H/cirrusux-setup ftps-passwd < /dev/null > $T/out 2>&1 && fail "ftps-passwd ran without a terminal"
grep -q "in a terminal" $T/out || fail "wrong refusal"
$H/cirrusux-setup check ftps > /dev/null 2>&1 || fail "ftps re-check"
echo "5 ftps: TLS required, passive ports, no anonymous, own cert; the password only asked for"
