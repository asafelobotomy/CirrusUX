#!/bin/bash
# Run by run.sh inside `unshare -rn`: root in a private user and network
# namespace, so the real firewall is never touched.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/root/etc/cirrusux
cat > $T/stub/systemctl <<'S'
#!/bin/sh
f="$STUBDIR/enabled"; touch "$f"
case "$1" in
is-enabled) grep -qx "$3" "$f" ;;
enable) echo "$2" >> "$f" ;;
*) exit 0 ;;
esac
S
# nft itself is real here; the package is reported as installed.
printf '#!/bin/sh\nprintf "install ok installed"\n' > $T/stub/dpkg-query
chmod +x $T/stub/systemctl $T/stub/dpkg-query
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 NO_SYSTEMD=1 CONFIG=$T/root/etc/cirrusux/host.env
printf 'FIREWALL_ALLOW="192.168.1.0/24 fd00::/8"\nFIREWALL_CONFIRM_SECONDS=3\nFIREWALL_PUBLIC_TCP="80 443"\n' > $CONFIG
fail() { echo "FAIL: $*"; exit 1; }

$H/cirrusux-setup check firewall > $T/out; rc=$?
[ $rc = 2 ] || fail "check exit $rc"; grep -q "policy drop" $T/out || fail "check shows no ruleset"
echo "1 check reports the change"

$H/cirrusux-setup apply firewall >/dev/null || fail apply
nft list table inet cirrusux | grep -q "policy drop" || fail "not loaded"
[ -f $ROOT/var/lib/cirrusux/firewall/pending ] || fail "no pending marker"
echo "2 apply loads it"
# The rollback is due after 3 s; allow a busy machine up to 20.
for _ in $(seq 40); do nft list table inet cirrusux >/dev/null 2>&1 || break; sleep 0.5; done
nft list table inet cirrusux >/dev/null 2>&1 && fail "not rolled back"
echo "3 unconfirmed: rolled back by itself"

$H/cirrusux-setup apply firewall >/dev/null || fail apply2
$H/cirrusux-setup firewall-confirm >/dev/null || fail confirm
grep -q "FIREWALL_CHECKS=inet/cirrusux/input:drop" $ROOT/etc/cirrusux/helper.d/firewall.env || fail "panel checks not written on confirm"
sleep 5; sleep 1
nft list table inet cirrusux >/dev/null 2>&1 || fail "confirmed ruleset vanished"
[ -f $ROOT/etc/cirrusux/firewall.nft ] || fail "not saved"
grep -qx cirrusux-firewall.service $T/enabled || fail "boot unit not enabled"
echo "4 confirmed: kept, saved, loaded at boot"

$H/cirrusux-setup check firewall > $T/out; rc=$?
[ $rc = 0 ] || { cat $T/out; fail "second check exit $rc"; }
echo "5 re-check: nothing to change"

printf 'FIREWALL_ALLOW="192.168.1.0/24"\nFIREWALL_TAILSCALE=no\nFIREWALL_CONFIRM_SECONDS=3\n' > $CONFIG
SSH_CONNECTION="203.0.113.5 5000 192.168.1.2 22" $H/cirrusux-setup apply firewall > $T/out 2>&1 && fail "lockout not refused"
grep -q "would be cut off" $T/out || fail "wrong lockout message"
SSH_CONNECTION="192.168.1.77 5000 192.168.1.2 22" $H/cirrusux-setup check firewall > /dev/null; [ $? = 2 ] || fail "allowed client refused"
echo "6 lockout guard: refuses an SSH client outside the allowed networks"

$H/cirrusux-setup apply firewall >/dev/null
$H/cirrusux-setup firewall-rollback >/dev/null || fail rollback
nft list table inet cirrusux | grep -q "100.64.0.0/10" || fail "manual rollback didn't restore the confirmed table"
echo "7 manual rollback restores the earlier ruleset"
