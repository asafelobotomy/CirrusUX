package gateway

import (
	"net"
	"testing"
)

func wakeOpts(configured, reported string) *Power {
	o := PowerOptions{Split: true, Wake: "wol", WakeBroadcast: "127.0.0.1:9"}
	if configured != "" {
		o.WakeMAC, _ = net.ParseMAC(configured)
	}
	if reported != "" || configured == "" {
		o.ReportedMAC = func() string { return reported }
	}
	return NewPower(o)
}

func TestWakeMAC(t *testing.T) {
	cases := []struct{ name, configured, reported, want string }{
		{"reported", "", "aa:bb:cc:dd:ee:ff", "aa:bb:cc:dd:ee:ff"},
		{"configured wins", "00:11:22:33:44:55", "aa:bb:cc:dd:ee:ff", "00:11:22:33:44:55"},
		{"none reported", "", "", ""},
		{"malformed", "", "nonsense", ""},
		{"eight bytes", "", "00:11:22:33:44:55:66:77", ""},
		{"broadcast", "", "ff:ff:ff:ff:ff:ff", ""},
		{"multicast", "", "01:00:5e:00:00:01", ""},
	}
	for _, c := range cases {
		got := ""
		if m := wakeOpts(c.configured, c.reported).wakeMAC(); m != nil {
			got = m.String()
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// No ReportedMAC func at all (older callers).
	if m := NewPower(PowerOptions{Wake: "wol"}).wakeMAC(); m != nil {
		t.Errorf("no source: %v", m)
	}
}

func TestWakeFailsWithoutMAC(t *testing.T) {
	p := wakeOpts("", "")
	done := make(chan error, 1)
	if !p.Wake(func(err error) { done <- err }) {
		t.Fatal("wake refused")
	}
	err := <-done
	want := "the server hasn't reported its MAC address yet: set CIRRUS_WAKE_MAC in gateway.env"
	if err == nil || err.Error() != want {
		t.Errorf("got %v", err)
	}
}

func TestWakeSendsReportedMAC(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer pc.Close()
	p := wakeOpts("", "aa:bb:cc:dd:ee:ff")
	p.opt.WakeBroadcast = pc.LocalAddr().String()
	failed := make(chan error, 1)
	p.Wake(func(err error) { failed <- err })
	buf := make([]byte, 200)
	n, _, err := pc.ReadFrom(buf)
	if err != nil || n != 102 {
		t.Fatalf("packet: %d bytes, %v", n, err)
	}
	if got := net.HardwareAddr(buf[6:12]).String(); got != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("packet MAC %s", got)
	}
	select {
	case err := <-failed:
		t.Errorf("unexpected failure: %v", err)
	default:
	}
}
