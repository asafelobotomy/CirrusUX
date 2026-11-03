package gateway

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"
)

// Wake sends the configured wake in the background: a magic packet, or
// the wake command. It reports false if a wake is already running. onFail,
// if set, is called with the error when the wake can't be sent.
func (p *Power) Wake(onFail func(error)) bool {
	p.mu.Lock()
	if p.waking != nil {
		p.mu.Unlock()
		return false
	}
	w := &wakeState{started: time.Now(), step: 1}
	p.waking = w
	p.mu.Unlock()

	fail := func(err error) {
		p.mu.Lock()
		w.err = "The wake didn't go out (" + err.Error() + ")"
		p.mu.Unlock()
		if onFail != nil {
			onFail(err)
		}
	}
	switch {
	case p.opt.Wake == "wol":
		go func() {
			if err := sendMagicPacket(p.opt.WakeMAC, p.opt.WakeBroadcast); err != nil {
				slog.Warn("magic packet", "err", err)
				fail(err)
			}
		}()
	case p.opt.Wake == "command":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), wakeTimeout)
			defer cancel()
			out, err := exec.CommandContext(ctx, p.opt.WakeCmd[0], p.opt.WakeCmd[1:]...).CombinedOutput()
			if err != nil {
				slog.Warn("wake command failed", "err", err, "output", strings.TrimSpace(string(out)))
				fail(err)
				return
			}
			p.mu.Lock()
			if w.step < 2 {
				w.step = 2
			}
			p.mu.Unlock()
		}()
	default:
		// Development: pretend the packet went out.
		go func() {
			time.Sleep(2 * time.Second)
			p.mu.Lock()
			w.step = 2
			p.mu.Unlock()
		}()
	}
	return true
}

// sendMagicPacket broadcasts a Wake-on-LAN packet: six 0xFF bytes, then the
// MAC address 16 times. It goes out three times, as single UDP packets can
// be lost. Go sets SO_BROADCAST on UDP sockets, so a broadcast address works.
func sendMagicPacket(mac net.HardwareAddr, addr string) error {
	pkt := bytes.Repeat([]byte{0xff}, 6)
	for range 16 {
		pkt = append(pkt, mac...)
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	for range 3 {
		if _, err := conn.Write(pkt); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
