#!/bin/bash
# Run by run.sh (no root or namespace needed): scripts/sign-release.sh signs
# a release with a key held only by an ssh-agent, refuses to start when the
# agent doesn't offer the key, and scripts/verify-release.sh accepts the
# signed release and refuses a changed, unlisted or wrongly signed one.
# GitHub is a folder here: gh and curl are stand-ins that copy files.
set -u
H=$(cd "$(dirname "$0")/../../.." && pwd)
T=$(mktemp -d)
agent_pid=""
trap '[ -n "$agent_pid" ] && kill "$agent_pid" 2>/dev/null; rm -rf "$T"' EXIT
fail() { echo "FAIL: $*"; exit 1; }
mkdir -p "$T/stub" "$T/gh/v1.2.3"

# gh release download V -R REPO -D DIR, gh release upload V FILE -R REPO --clobber
cat >"$T/stub/gh" <<'S'
#!/bin/sh
case "$1 $2" in
"release download") mkdir -p "$7" && cp "$GHDIR/$3"/* "$7"/ ;;
"release upload") cp "$4" "$GHDIR/$3/" ;;
*) echo "gh stand-in: $*" >&2; exit 1 ;;
esac
S
# curl ... -o FILE https://github.com/REPO/releases/download/V/NAME
cat >"$T/stub/curl" <<'S'
#!/bin/sh
for a; do case "$a" in -o) next=o ;; *) [ "${next:-}" = o ] && out=$a; next= ;; esac; url=$a; done
v=${url%/*}; v=${v##*/}
cp "$GHDIR/$v/${url##*/}" "$out" 2>/dev/null
S
chmod +x "$T/stub"/*
export PATH=$T/stub:$PATH GHDIR=$T/gh

ssh-keygen -q -t ed25519 -N '' -C test -f "$T/key"
ssh-keygen -q -t ed25519 -N '' -C other -f "$T/other"
# The quotes are part of the allowed-signers line, not shell quoting.
# shellcheck disable=SC2089,SC2090
{ CIRRUS_SIGNERS="release@cirrusux namespaces=\"cirrusux-release\" $(cut -d" " -f1,2 "$T/key.pub")"; export CIRRUS_SIGNERS; }

# A published, unsigned release.
R=$T/gh/v1.2.3
echo bundle >"$R/cirrusux-v1.2.3-linux-amd64.tar.gz"
cp "$H/install.sh" "$R/install.sh"
(cd "$R" && sha256sum ./*.tar.gz install.sh | sed 's| \./| |' >SHA256SUMS)

eval "$(ssh-agent -s -a "$T/agent.sock")" >/dev/null
agent_pid=$SSH_AGENT_PID

# 1. An agent without the key: refused before anything is downloaded.
ssh-add -q "$T/other" 2>/dev/null
"$H/scripts/sign-release.sh" --check-agent >"$T/out" 2>&1 && fail "--check-agent passed without the key"
grep -q "doesn't offer the release key" "$T/out" || fail "no clear message for a missing key: $(cat "$T/out")"
grep -q "ED25519" "$T/out" || fail "it should list what the agent offers: $(cat "$T/out")"
"$H/scripts/sign-release.sh" v1.2.3 --no-attestation >"$T/out" 2>&1 && fail "signed without the key"
[ ! -e "$R/SHA256SUMS.sig" ] || fail "a signature was uploaded without the key"
(unset SSH_AUTH_SOCK; "$H/scripts/sign-release.sh" --check-agent >"$T/out" 2>&1) && fail "--check-agent passed with no agent"
grep -q "SSH_AUTH_SOCK is unset" "$T/out" || fail "no clear message for no agent: $(cat "$T/out")"
echo "1 an agent without the release key is refused before signing"

# 2. Unsigned: verify refuses it.
"$H/scripts/verify-release.sh" v1.2.3 >"$T/out" 2>&1 && fail "verified an unsigned release"
grep -q "isn't signed" "$T/out" || fail "unsigned: $(cat "$T/out")"
echo "2 an unsigned release doesn't verify"

# 3. With the key in the agent: signed, uploaded, and checked from "GitHub".
ssh-add -q "$T/key" 2>/dev/null
"$H/scripts/sign-release.sh" --check-agent >"$T/out" 2>&1 || fail "--check-agent with the key: $(cat "$T/out")"
"$H/scripts/sign-release.sh" v1.2.3 --no-attestation >"$T/out" 2>&1 || fail "signing: $(cat "$T/out")"
[ -s "$R/SHA256SUMS.sig" ] || fail "no SHA256SUMS.sig uploaded"
grep -q "Signature: SHA256SUMS is signed" "$T/out" || fail "sign-release.sh didn't check the upload: $(cat "$T/out")"
"$H/scripts/verify-release.sh" v1.2.3 >"$T/out" 2>&1 || fail "verify by version: $(cat "$T/out")"
"$H/scripts/verify-release.sh" --from "$R" >"$T/out" 2>&1 || fail "verify --from: $(cat "$T/out")"
grep -q "would install" "$T/out" || fail "verify --from: $(cat "$T/out")"
echo "3 signed with the agent's key; verify-release.sh accepts it"

# 4. What verify must refuse.
cp -r "$R" "$T/changed"; echo tampered >"$T/changed/cirrusux-v1.2.3-linux-amd64.tar.gz"
"$H/scripts/verify-release.sh" --from "$T/changed" >/dev/null 2>&1 && fail "verified a changed bundle"
cp -r "$R" "$T/extra"; echo x >"$T/extra/extra.sh"
"$H/scripts/verify-release.sh" --from "$T/extra" >"$T/out" 2>&1 && fail "verified a release with an unlisted file"
grep -q "not in SHA256SUMS" "$T/out" || fail "unlisted file: $(cat "$T/out")"
cp -r "$R" "$T/wrongkey"; rm "$T/wrongkey/SHA256SUMS.sig"
ssh-keygen -q -Y sign -f "$T/other" -n cirrusux-release "$T/wrongkey/SHA256SUMS" 2>/dev/null
"$H/scripts/verify-release.sh" --from "$T/wrongkey" >/dev/null 2>&1 && fail "verified a signature from another key"
cp -r "$R" "$T/wrongns"; rm "$T/wrongns/SHA256SUMS.sig"
ssh-keygen -q -Y sign -f "$T/key" -n file "$T/wrongns/SHA256SUMS" 2>/dev/null
"$H/scripts/verify-release.sh" --from "$T/wrongns" >/dev/null 2>&1 && fail "verified a signature in another namespace"
echo "4 a changed bundle, an unlisted file, another key and another namespace are refused"
