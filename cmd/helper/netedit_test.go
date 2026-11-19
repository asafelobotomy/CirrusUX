package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/fsutil"
	"cirrusux/internal/helperapi"
)

func netTestRoot(t *testing.T) (root string, calls *[]string) {
	root = t.TempDir()
	old, oldRun := accessRoot, netRun
	accessRoot = root
	var log []string
	netRun = func(_ context.Context, name string, args ...string) ([]byte, error) {
		log = append(log, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() { accessRoot, netRun = old, oldRun })
	os.MkdirAll(filepath.Join(root, "var/lib/cirrusux"), 0o755)
	return root, &log
}

func TestNetDNSRollsBack(t *testing.T) {
	root, calls := netTestRoot(t)
	ctx := context.Background()
	drop := filepath.Join(root, dnsDropin)
	if _, err := netDNS(ctx, "192.0.2.53", "lan"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(drop)
	if !strings.Contains(string(b), "DNS=192.0.2.53") || !strings.Contains(string(b), "Domains=lan") {
		t.Errorf("drop-in: %q", b)
	}
	if !slicesHas(*calls, "systemd-run --quiet --collect --unit="+helperapi.NetRevertUnit) {
		t.Errorf("no revert timer: %v", *calls)
	}
	// A second change waits for the first.
	if _, err := netDNS(ctx, "192.0.2.54", "-"); err == nil {
		t.Error("two changes at once")
	}
	if _, err := netRevert(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(drop); err == nil {
		t.Error("the drop-in survived the revert")
	}
	if readPending() != nil {
		t.Error("pending record left after the revert")
	}
}

func TestNetConfirmKeeps(t *testing.T) {
	root, calls := netTestRoot(t)
	ctx := context.Background()
	os.MkdirAll(filepath.Join(root, "etc/systemd/resolved.conf.d"), 0o755)
	os.WriteFile(filepath.Join(root, dnsDropin), []byte("[Resolve]\nDNS=192.0.2.1\n"), 0o644)
	if _, err := netDNS(ctx, "192.0.2.53", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err := netConfirm(ctx); err != nil {
		t.Fatal(err)
	}
	if readPending() != nil || !slicesHas(*calls, "systemctl stop "+helperapi.NetRevertUnit+".timer") {
		t.Errorf("confirm: %v", *calls)
	}
	if st := netStatus(ctx); len(st.DNS) != 1 || st.DNS[0] != "192.0.2.53" {
		t.Errorf("status %+v", st)
	}
	// A change left from before a reboot is undone before a new one starts.
	stale := helperapi.NetPending{Kind: "dns", What: "x", Deadline: time.Now().Add(-time.Hour), DNSHad: true, DNSPrev: "[Resolve]\nDNS=192.0.2.1\n"}
	if err := writeJSONTest(pendingPath(), stale); err != nil {
		t.Fatal(err)
	}
	if _, err := netDNS(ctx, "192.0.2.60", "-"); err != nil {
		t.Fatal(err)
	}
	if p := readPending(); p == nil || p.DNSPrev == "" || !strings.Contains(p.DNSPrev, "192.0.2.1") {
		t.Errorf("the stale change wasn't rolled back first: %+v", p)
	}
}

func TestNetArgs(t *testing.T) {
	for _, a := range [][]string{{helperapi.CmdNetDNS, "192.0.2.53,2001:db8::1", "lan"}, {helperapi.CmdNetDNS, "-", "-"}, {helperapi.CmdNetTailscale, "yes", "no"}} {
		if !validArgs(a) {
			t.Errorf("refused %v", a)
		}
	}
	for _, a := range [][]string{{helperapi.CmdNetDNS, "1.1.1.1;ls", "-"}, {helperapi.CmdNetDNS, "1.1.1.1"}, {helperapi.CmdNetTailscale, "yes", "maybe"}, {helperapi.CmdAccountCreate, "alice", "root"}, {helperapi.CmdAccountRemove, "Alice"}} {
		if validArgs(a) {
			t.Errorf("accepted %v", a)
		}
	}
	if !validArgs([]string{helperapi.CmdAccountCreate, "alice", "admin"}) {
		t.Error("refused account-create")
	}
}

func slicesHas(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func writeJSONTest(path string, v helperapi.NetPending) error {
	return fsutil.WriteJSON(path, v, 0o644)
}

func TestAccountCreateRemoveRefusals(t *testing.T) {
	root := t.TempDir()
	old := accessRoot
	accessRoot = root
	t.Cleanup(func() { accessRoot = old })
	os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	os.WriteFile(filepath.Join(root, "etc/passwd"), []byte("root:x:0:0::/root:/bin/bash\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\nbob:x:1001:1001:CirrusUX panel:/home/bob:/bin/bash\n"), 0o644)
	os.WriteFile(filepath.Join(root, "etc/group"), []byte("sudo:x:27:alice\nstaff:x:50:\n"), 0o644)
	os.WriteFile(filepath.Join(root, "etc/shadow"), []byte("alice:$y$x:1:0:99999:7:::\nbob:$y$x:1:0:99999:7:::\n"), 0o644)
	ctx := context.Background()
	if _, err := accountCreate(ctx, "alice", "user"); err == nil {
		t.Error("created an account over an existing one")
	}
	if _, err := accountCreate(ctx, "staff", "user"); err == nil {
		t.Error("created an account named like a group")
	}
	if _, err := accountRemove(ctx, "alice"); err == nil || !strings.Contains(err.Error(), "created from the panel") {
		t.Errorf("removing an account the panel didn't make: %v", err)
	}
	if _, err := accountRemove(ctx, "root"); err == nil {
		t.Error("removed root")
	}
}
