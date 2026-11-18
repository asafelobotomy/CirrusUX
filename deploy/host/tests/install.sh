#!/bin/bash
# Run by run.sh inside `unshare -r`: install.sh from a downloaded release
# folder, a damaged, unsigned or wrongly signed bundle refused, then
# cirrusux-setup upgrade and rollback, all against a scratch ROOT and
# prefix with stand-in tools. Releases are signed with a key made here.
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; mkdir -p $T/stub $T/root/etc/cirrusux $T/b
case "$(uname -m)" in x86_64) A=amd64 ;; aarch64) A=arm64 ;; *) echo "skipping: no bundles for $(uname -m)"; exit 0 ;; esac
cat > $T/stub/systemctl <<'S'
#!/bin/sh
f="$STUBDIR/enabled"; touch "$f"; echo "systemctl $*" >> "$STUBDIR/calls"
case "$1" in
is-enabled|is-active) grep -qx "$3" "$f" ;;
enable) shift; [ "$1" = --now ] && shift; for u; do echo "$u" >> "$f"; done ;;
*) exit 0 ;;
esac
S
printf '#!/bin/sh\n[ "$1" = cirrusux ] && exit 0\nexec /usr/bin/id "$@"\n' > $T/stub/id
printf '#!/bin/sh\nprintf "install ok installed"\n' > $T/stub/dpkg-query
chmod +x $T/stub/*
export STUBDIR=$T PATH=$T/stub:$PATH ROOT=$T/root NO_CHOWN=1 CONFIG=$T/root/etc/cirrusux/host.env CIRRUS_PREFIX=$T/opt
unset CIRRUS_RELEASE
fail() { echo "FAIL: $*"; exit 1; }
ssh-keygen -q -t ed25519 -N '' -C test -f $T/key; ssh-keygen -q -t ed25519 -N '' -C other -f $T/other
export CIRRUS_SIGNERS="release@cirrusux namespaces=\"cirrusux-release\" $(cut -d' ' -f1,2 $T/key.pub)"
# sign DIR [KEY]: sign DIR/SHA256SUMS as scripts/sign-release.sh does.
sign() { rm -f $1/SHA256SUMS.sig; ssh-keygen -q -Y sign -f ${2:-$T/key} -n cirrusux-release $1/SHA256SUMS 2>/dev/null; }

# bundle V: a release folder $T/rV like the one GitHub serves, built from
# this checkout's deploy/ and install.sh, with stand-in binaries.
bundle() {
	local n=cirrusux-v$1-linux-$A
	mkdir -p $T/b/$n/bin $T/r$1
	cp -r $H/.. $T/b/$n/deploy; cp $H/../../install.sh $T/b/$n/
	echo $1 > $T/b/$n/VERSION
	for b in agent helper dockerfilter gateway idle; do echo "binary $b $1" > $T/b/$n/bin/cirrusux-$b; done
	tar -C $T/b -czf $T/r$1/$n.tar.gz $n
	(cd $T/r$1 && sha256sum $n.tar.gz > SHA256SUMS)
	sign $T/r$1
}
bundle 0.1.0; bundle 0.2.0
printf 'HOST_MODULES="cirrusux"\nCIRRUS_ROLE=single\nCIRRUS_ALLOWED_USERS=me@example.com\n' > $CONFIG

# A bundle that doesn't match its checksum never lands.
mkdir $T/bad; cp $T/r0.1.0/* $T/bad/; echo x >> $T/bad/*.tar.gz
bash $H/../../install.sh --from $T/bad --no-init > $T/out 2>&1 && fail "damaged bundle installed"
grep -q "doesn't match its checksum" $T/out && [ ! -e $T/opt/current ] || fail "damaged bundle: $(cat $T/out)"
echo "1 a bundle that fails its checksum isn't unpacked"

# refused DIR WHY [MESSAGE]: install.sh must not unpack the release in DIR.
refused() {
	bash $H/../../install.sh --from $1 --no-init > $T/out 2>&1 && fail "$2: installed"
	local want="isn't signed with the CirrusUX release key"
	[ -z "${3:-}" ] || want=$3
	grep -q "$want" $T/out && [ ! -e $T/opt/current ] || fail "$2: $(cat $T/out)"
}
mkdir $T/nosig; cp $T/r0.1.0/*.tar.gz $T/r0.1.0/SHA256SUMS $T/nosig/
refused $T/nosig "an unsigned release" "isn't signed (no SHA256SUMS.sig)"
mkdir $T/wrongkey; cp $T/nosig/* $T/wrongkey/; sign $T/wrongkey $T/other
refused $T/wrongkey "a release signed with another key"
# A changed bundle with SHA256SUMS redone to match: the old signature no longer fits.
mkdir $T/swapped; cp $T/r0.1.0/* $T/swapped/; echo x >> $T/swapped/*.tar.gz
(cd $T/swapped && sha256sum *.tar.gz > SHA256SUMS)
refused $T/swapped "a changed bundle with a rewritten SHA256SUMS"
# Signed for something else than a release (another namespace).
mkdir $T/otherns; cp $T/nosig/* $T/otherns/; ssh-keygen -q -Y sign -f $T/key -n file $T/otherns/SHA256SUMS 2>/dev/null
refused $T/otherns "a signature made for another purpose"
bash $H/../../install.sh --from $T/nosig --no-init --unsigned > $T/out 2>&1 || { cat $T/out; fail "--unsigned"; }
grep -q "Not checking the signature" $T/out && [ "$(readlink $T/opt/current)" = releases/v0.1.0 ] || fail "--unsigned: $(cat $T/out)"
rm -rf $T/opt
echo "1b unsigned, wrongly signed and re-hashed releases aren't unpacked; --unsigned says so and installs"

bash $H/../../install.sh --from $T/r0.1.0 > $T/out 2>&1 || { cat $T/out; fail "install"; }
grep -q "Signature matches the release key" $T/out || fail "the signature wasn't checked: $(cat $T/out)"
[ "$(readlink $T/opt/current)" = releases/v0.1.0 ] || fail "current link"
[ -z "$(find $T/opt -perm /022 ! -type l)" ] || fail "release writable by group or others: $(find $T/opt -perm /022 ! -type l | head -3)"
grep -q "already set up" $T/out || fail "an existing host.env should stop init: $(cat $T/out)"
$T/opt/current/deploy/host/cirrusux-setup apply > $T/out 2>&1 || { cat $T/out; fail "apply v0.1.0"; }
grep -q "binary agent 0.1.0" $ROOT/usr/local/bin/cirrusux-agent || fail "v0.1.0 binary not installed"
echo "2 install.sh checks, unpacks to releases/v0.1.0 and links current"

CIRRUS_UPGRADE_FROM=$T/r0.2.0 $ROOT/usr/local/sbin/cirrusux-setup upgrade -y > $T/out 2>&1 || { cat $T/out; fail "upgrade"; }
[ "$(readlink $T/opt/current)" = releases/v0.2.0 ] && [ "$(readlink $T/opt/previous)" = releases/v0.1.0 ] || fail "links after upgrade"
grep -q "binary agent 0.2.0" $ROOT/usr/local/bin/cirrusux-agent || fail "upgrade didn't install the new binary"
[ "$(readlink $ROOT/usr/local/sbin/cirrusux-setup)" = "$T/opt/releases/v0.2.0/deploy/host/cirrusux-setup" ] || fail "setup link not moved"
grep -q "upgrade: v0.1.0 → v0.2.0" $ROOT/var/lib/cirrusux/setup.log || fail "upgrade not logged"
echo "3 upgrade unpacks the new release, applies it, keeps the old one"

CIRRUS_UPGRADE_FROM=$T/r0.2.0 $ROOT/usr/local/sbin/cirrusux-setup upgrade < /dev/null > $T/out 2>&1; rc=$?
[ $rc = 0 ] && grep -q "Already on v0.2.0" $T/out || fail "upgrade to the same version ($rc): $(cat $T/out)"
$ROOT/usr/local/sbin/cirrusux-setup rollback -y > $T/out 2>&1 || { cat $T/out; fail "rollback"; }
[ "$(readlink $T/opt/current)" = releases/v0.1.0 ] && [ "$(readlink $T/opt/previous)" = releases/v0.2.0 ] || fail "links after rollback"
grep -q "binary agent 0.1.0" $ROOT/usr/local/bin/cirrusux-agent || fail "rollback didn't put the old binary back"
echo "4 rollback goes back to the release before, and can be undone"

bundle 0.3.0
CIRRUS_UPGRADE_FROM=$T/r0.3.0 $ROOT/usr/local/sbin/cirrusux-setup upgrade < /dev/null > $T/out 2>&1
grep -q "not applied: run it again with -y" $T/out || fail "upgrade without a terminal or -y should stop before apply: $(cat $T/out)"
grep -q "binary agent 0.1.0" $ROOT/usr/local/bin/cirrusux-agent || fail "applied without asking"
ls $T/opt/releases | tr '\n' ' ' | grep -qx "v0.1.0 v0.3.0 " || fail "old releases not pruned: $(ls $T/opt/releases)"
echo "5 without -y it stops before applying; only current and previous are kept"
