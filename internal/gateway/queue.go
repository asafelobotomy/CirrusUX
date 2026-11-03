package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	pollWait    = 25 * time.Second
	pendingMax  = 2 * time.Minute // agent never asked: server asleep or agent down
	sentMax     = 4 * time.Minute // agent took it but didn't report (egress report: up to 3 min)
	keepHistory = 50
)

// Command is an action on its way to the agent.
type Command struct {
	ID string `json:"id"`
	ActionReq
	Label    string     `json:"label"`
	Who      string     `json:"who"`
	Created  time.Time  `json:"created"`
	State    string     `json:"state"` // pending, sent, done, failed
	Output   []string   `json:"output,omitempty"`
	Error    string     `json:"error,omitempty"`
	Undo     *ActionReq `json:"undo,omitempty"`
	ReadOnly bool       `json:"-"`
	sent     time.Time
}

// Queue holds commands in memory. A gateway restart drops pending ones,
// which then show as failed in the UI; nothing runs twice.
type Queue struct {
	mu     sync.Mutex
	cmds   []*Command
	notify chan struct{}
}

func NewQueue() *Queue { return &Queue{notify: make(chan struct{}, 1)} }

func (q *Queue) Add(c *Command) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	c.ID = hex.EncodeToString(b)
	c.State, c.Created = "pending", time.Now()
	q.mu.Lock()
	q.cmds = append(q.cmds, c)
	if len(q.cmds) > keepHistory {
		q.cmds = q.cmds[len(q.cmds)-keepHistory:]
	}
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *Queue) Get(id string) (Command, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.expire(time.Now())
	for _, c := range q.cmds {
		if c.ID == id {
			return *c, true
		}
	}
	return Command{}, false
}

// Next waits up to pollWait for a pending command and marks it sent.
func (q *Queue) Next(ctx context.Context) *Command {
	timer := time.NewTimer(pollWait)
	defer timer.Stop()
	for {
		q.mu.Lock()
		q.expire(time.Now())
		for _, c := range q.cmds {
			if c.State == "pending" {
				c.State, c.sent = "sent", time.Now()
				cc := *c
				q.mu.Unlock()
				return &cc
			}
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return nil
		case <-q.notify:
		}
	}
}

// Complete records the agent's result. It returns the finished command, or
// false if it was unknown or had already expired.
func (q *Queue) Complete(id string, ok bool, output []string, errMsg string) (Command, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, c := range q.cmds {
		if c.ID == id && c.State == "sent" {
			c.Output, c.Error = output, errMsg
			c.State = "done"
			if !ok {
				c.State = "failed"
			}
			return *c, true
		}
	}
	return Command{}, false
}

func (q *Queue) expire(now time.Time) {
	for _, c := range q.cmds {
		switch {
		case c.State == "pending" && now.Sub(c.Created) > pendingMax:
			c.State, c.Error = "failed", "the server didn't pick it up (asleep, or the agent isn't running)"
		case c.State == "sent" && now.Sub(c.sent) > sentMax:
			c.State, c.Error = "failed", "the server took it but didn't report back"
		}
	}
}
