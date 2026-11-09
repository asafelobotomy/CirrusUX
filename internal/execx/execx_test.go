package execx

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesAndCaps(t *testing.T) {
	out, err := Run(context.Background(), Options{}, "echo", "hi")
	if err != nil || string(out) != "hi\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	out, err = Run(context.Background(), Options{MaxOutput: 10}, "sh", "-c", "yes | head -c 100000")
	if err != nil || len(out) != 10 {
		t.Errorf("cap: got %d bytes, %v", len(out), err)
	}
}

func TestRunStderrOnlyWhenCombined(t *testing.T) {
	out, _ := Run(context.Background(), Options{}, "sh", "-c", "echo out; echo err >&2")
	if strings.Contains(string(out), "err") {
		t.Errorf("stderr kept: %q", out)
	}
	out, _ = Run(context.Background(), Options{Combined: true}, "sh", "-c", "echo out; echo err >&2")
	if !strings.Contains(string(out), "err") {
		t.Errorf("stderr lost: %q", out)
	}
}

func TestRunTimeout(t *testing.T) {
	start := time.Now()
	_, err := Run(context.Background(), Options{Timeout: 100 * time.Millisecond}, "sleep", "5")
	if err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("not stopped in time: %v after %v", err, time.Since(start))
	}
}

func TestRunEnv(t *testing.T) {
	out, _ := Run(context.Background(), Options{Env: []string{"EXECX_T=yes"}}, "sh", "-c", "echo $EXECX_T")
	if string(out) != "yes\n" {
		t.Errorf("got %q", out)
	}
}
