package appjob

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The check is asked on the address ports are published on: with
// APPS_BIND set nothing listens on loopback, so asking 127.0.0.1 would
// fail every install and roll back every update.
func TestHealthCheckUsesBindAddress(t *testing.T) {
	for _, c := range []struct{ bind, want string }{
		{"", "http://127.0.0.1:4533/"},
		{"0.0.0.0", "http://127.0.0.1:4533/"},
		{"192.0.2.10", "http://192.0.2.10:4533/"},
		{"2001:db8::5", "http://[2001:db8::5]:4533/"},
	} {
		e, _, media := testEnv(t)
		e.Host.BindAddress = c.bind
		var asked string
		e.Healthy = func(_ context.Context, url string) error { asked = url; return nil }
		if err := e.Run(context.Background(), navidrome(media)); err != nil {
			t.Fatalf("bind %q: %v", c.bind, err)
		}
		if !strings.HasPrefix(asked, c.want) {
			t.Errorf("bind %q: asked %q, want %s…", c.bind, asked, c.want)
		}
	}
}

// A refusal is recorded at once, even while another job of the same app
// holds the lock.
func TestRefuseDoesNotWaitForTheLock(t *testing.T) {
	e, _, media := testEnv(t)
	unlock, err := e.lockApp("navidrome")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	done := make(chan error, 1)
	go func() { done <- e.Refuse(navidrome(media), errors.New("port taken")) }()
	select {
	case err := <-done:
		if err == nil || err.Error() != "port taken" {
			t.Errorf("Refuse = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Refuse waited for the app's lock")
	}
	if s := status(t, e, "aaaa1111"); s.State != "failed" || s.Error != "port taken" || s.Finished == nil {
		t.Errorf("status = %+v", s)
	}
}

// A line longer than the scanner takes must not leave the command blocked
// writing to the pipe.
func TestRunCommandSurvivesLongLines(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var lines []string
	err := RunCommand(ctx, func(l string) { lines = append(lines, l) }, "sh", "-c",
		`echo first; head -c 3000000 /dev/zero | tr '\0' a; echo; echo last`)
	if ctx.Err() != nil {
		t.Fatal("command hung on a long line")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 || lines[0] != "first" {
		t.Errorf("lines = %d, first %q", len(lines), lines)
	}
}

// A job shows as running from the moment it's accepted, before its unit
// starts, so the agent's report straight after the action already has it
// and keeps reporting every few seconds until it's done.
func TestQueueShowsTheJobRunningBeforeItStarts(t *testing.T) {
	e, _, media := testEnv(t)
	r := navidrome(media)
	if err := e.Queue(r); err != nil {
		t.Fatal(err)
	}
	if st := status(t, e, r.Job); st.State != "running" || st.Name != "Navidrome" || st.Step == "" {
		t.Errorf("queued status = %+v; want running, with a name and a step", st)
	}
	if err := e.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if st := status(t, e, r.Job); st.State != "done" {
		t.Errorf("after the run: %+v", st)
	}
}
