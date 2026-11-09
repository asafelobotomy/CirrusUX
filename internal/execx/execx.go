// Package execx runs external commands with a time limit and a cap on how
// much output is kept. A command that hangs or floods stdout can't stall
// the caller or fill its memory.
package execx

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"
)

// DefaultMaxOutput is how much output is kept when Options.MaxOutput is 0.
const DefaultMaxOutput = 4 << 20

// Options shape one run. The zero value is: no extra time limit beyond the
// context, 4 MB of stdout kept, stderr dropped, the caller's environment.
type Options struct {
	Timeout   time.Duration // 0: only the context limits it
	MaxOutput int           // bytes kept; 0 is DefaultMaxOutput
	Combined  bool          // keep stderr too, interleaved with stdout
	Env       []string      // added to the caller's environment
}

// Run runs name with args and returns its output (cut at MaxOutput; the
// command still runs to the end, its extra output is discarded).
func Run(ctx context.Context, o Options, name string, args ...string) ([]byte, error) {
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}
	max := o.MaxOutput
	if max <= 0 {
		max = DefaultMaxOutput
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if len(o.Env) > 0 {
		cmd.Env = append(os.Environ(), o.Env...)
	}
	buf := &capped{max: max}
	cmd.Stdout = buf
	if o.Combined {
		cmd.Stderr = buf
	}
	// A hung child that holds the pipe open must not outlive the timeout.
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	return buf.b.Bytes(), err
}

// capped keeps the first max bytes written and swallows the rest.
type capped struct {
	b   bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		c.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
