package harness

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/tingly-dev/tingly-box/projects/llm"
)

// BrainFunc plays the coordinator's model: it gets the request and answers
// with text, tool calls, or the NO_UPDATE sentinel.
type BrainFunc func(c *BrainCtx) llm.Result

// BrainCtx is one request to the coordinator's model.
type BrainCtx struct {
	Req llm.Request
	// Err, when set, makes the request fail as an upstream error would.
	Err error
}

// Last is the text of the last user message.
func (c *BrainCtx) Last() string {
	for i := len(c.Req.Messages) - 1; i >= 0; i-- {
		if c.Req.Messages[i].Role == "user" {
			return c.Req.Messages[i].Content
		}
	}
	return ""
}

// AfterTools reports whether the request carries tool results (the turn's
// second round).
func (c *BrainCtx) AfterTools() bool {
	n := len(c.Req.Messages)
	return n > 0 && len(c.Req.Messages[n-1].ToolResults) > 0
}

// CheckIn reports whether this turn was woken by a thread's report.
func (c *BrainCtx) CheckIn() bool { return strings.Contains(c.Last(), "Coordinator check-in") }

// ThreadRef is the id prefix the coordinator's thread table shows for the
// titled thread ("" when the table has no such thread).
func (c *BrainCtx) ThreadRef(title string) string {
	m := regexp.MustCompile(`(?m)^- (\w+) ` + regexp.QuoteMeta(strconv.Quote(title))).FindStringSubmatch(c.Req.System)
	if m == nil {
		return ""
	}
	return m[1]
}

// Call builds a tool call.
func Call(id, name string, input any) llm.ToolCall {
	b, _ := json.Marshal(input)
	return llm.ToolCall{ID: id, Name: name, Input: b}
}

// NoUpdate is the coordinator's way of saying nothing.
const NoUpdate = "NO_UPDATE"

type brain struct {
	fn   BrainFunc
	mu   sync.Mutex
	reqs []llm.Request
}

func (b *brain) ClientFor(context.Context, string) (llm.Client, string, error) {
	return b, "coord-model", nil
}

func (b *brain) Stream(_ context.Context, req llm.Request, onDelta func(string)) (llm.Result, error) {
	b.mu.Lock()
	b.reqs = append(b.reqs, req)
	b.mu.Unlock()
	res := llm.Result{Text: NoUpdate}
	if b.fn != nil {
		c := &BrainCtx{Req: req}
		res = b.fn(c)
		if c.Err != nil {
			return llm.Result{}, c.Err
		}
	}
	if res.Text != "" {
		onDelta(res.Text)
	}
	return res, nil
}

func (b *brain) ListModels(context.Context) ([]llm.Model, error) { return nil, nil }

func (b *brain) requests() []llm.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]llm.Request(nil), b.reqs...)
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
