// Package grok talks to xAI's chat API (OpenAI-compatible) with a fixed
// set of tools. Read tools answer from the gateway's cached data; the one
// action tool only proposes, and the user confirms in the UI (PLAN §3.6).
package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cirrusux/internal/redact"
)

type Client struct {
	Base, Model, Key string
	HTTP             *http.Client
}

// Tool is one function Grok may call. Run returns plain data; it is JSON
// encoded and redacted before it leaves the gateway.
type Tool struct {
	Name        string
	Description string
	Params      map[string]any // JSON schema; nil means no parameters
	Run         func(args json.RawMessage) (any, error)
	// Proposal tools don't run anything: their call becomes a proposal for
	// the UI to confirm. Action names it (defaults to Name).
	Proposal bool
	Action   string
}

type Turn struct {
	Role string `json:"role"` // user, assistant
	Text string `json:"text"`
}

type Proposal struct {
	Action string            `json:"action"`
	Label  string            `json:"label"`
	Args   map[string]string `json:"args,omitempty"`
}

type Reply struct {
	Text      string    `json:"text"`
	Tools     []string  `json:"tools"`
	Proposal  *Proposal `json:"proposal,omitempty"`
	TokensIn  int       `json:"-"`
	TokensOut int       `json:"-"`
}

type message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

const maxRounds = 4

// Ask runs the conversation, letting Grok call tools for up to maxRounds.
// Everything sent — system prompt, user turns and tool results — passes
// through redact.String.
func (c *Client) Ask(ctx context.Context, system string, turns []Turn, tools []Tool) (Reply, error) {
	var reply Reply
	msgs := []message{{Role: "system", Content: redact.String(system)}}
	for _, t := range turns {
		if t.Role != "user" && t.Role != "assistant" {
			continue
		}
		msgs = append(msgs, message{Role: t.Role, Content: redact.String(t.Text)})
	}
	byName := map[string]Tool{}
	var specs []map[string]any
	for _, t := range tools {
		byName[t.Name] = t
		params := t.Params
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		specs = append(specs, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": params},
		})
	}

	for round := 0; round < maxRounds; round++ {
		res, err := c.complete(ctx, msgs, specs)
		if err != nil {
			return reply, err
		}
		reply.TokensIn += res.Usage.PromptTokens
		reply.TokensOut += res.Usage.CompletionTokens
		if len(res.Choices) == 0 {
			return reply, fmt.Errorf("empty response")
		}
		m := res.Choices[0].Message
		if len(m.ToolCalls) == 0 {
			reply.Text = strings.TrimSpace(m.Content)
			return reply, nil
		}
		msgs = append(msgs, message{Role: "assistant", Content: m.Content, ToolCalls: m.ToolCalls})
		for _, call := range m.ToolCalls {
			reply.Tools = append(reply.Tools, call.Function.Name)
			out := c.runTool(byName, call, &reply)
			msgs = append(msgs, message{Role: "tool", ToolCallID: call.ID, Content: redact.String(out)})
		}
	}
	return reply, fmt.Errorf("Grok used too many tool calls without answering")
}

func (c *Client) runTool(byName map[string]Tool, call toolCall, reply *Reply) string {
	t, ok := byName[call.Function.Name]
	if !ok {
		return `{"error":"unknown tool"}`
	}
	if t.Proposal {
		var raw map[string]any
		_ = json.Unmarshal([]byte(call.Function.Arguments), &raw)
		p := Proposal{Action: t.Action, Args: map[string]string{}}
		if p.Action == "" {
			p.Action = t.Name
		}
		for k, v := range raw {
			if k == "label" {
				p.Label, _ = v.(string)
			} else if s, ok := v.(string); ok {
				p.Args[k] = s
			}
		}
		if p.Label == "" {
			p.Label = t.Description
		}
		reply.Proposal = &p
		return `{"status":"proposed; the user will be asked to confirm in the panel"}`
	}
	v, err := t.Run(json.RawMessage(call.Function.Arguments))
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	b, _ := json.Marshal(v)
	return string(b)
}

type completion struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) complete(ctx context.Context, msgs []message, tools []map[string]any) (completion, error) {
	var out completion
	body, _ := json.Marshal(map[string]any{
		"model": c.Model, "messages": msgs, "tools": tools, "temperature": 0.2,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		// The error body may echo our request; keep only the status.
		return out, fmt.Errorf("xAI API: %s", resp.Status)
	}
	return out, json.Unmarshal(b, &out)
}
