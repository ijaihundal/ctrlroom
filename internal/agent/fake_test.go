package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

// E constructs an AgentEvent with optional key/value metadata pairs.
// Test-only helper: E(EventText, "hi", "k1", v1, "k2", v2).
func E(t EventType, content string, pairs ...any) AgentEvent {
	return AgentEvent{Type: t, Content: content, Metadata: MD(pairs...)}
}

// MD builds a metadata map from alternating key/value pairs. Test-only helper.
func MD(pairs ...any) map[string]any {
	if len(pairs)%2 != 0 {
		panic("MD requires even number of args")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		k, _ := pairs[i].(string) //nolint:errcheck // test helper: silent ignore on wrong type
		m[k] = pairs[i+1]
	}
	return m
}

// drainUntilDone reads events from ch until an EventDone is seen or ctx cancels.
// Returns the collected event types in order. Test-only helper for the FakeAdapter
// (whose channel does not auto-close on EventDone — multi-turn contract).
func drainUntilDone(ctx context.Context, ch <-chan AgentEvent) []EventType {
	var out []EventType
	for {
		select {
		case <-ctx.Done():
			return out
		case ev := <-ch:
			out = append(out, ev.Type)
			if ev.Type == EventDone {
				return out
			}
		}
	}
}

func TestFakeAdapter_DefaultScript(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f := NewFakeAdapter()
	if err := f.Spawn(ctx, t.TempDir(), AgentConfig{AgentType: "opencode"}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := f.SendPrompt(ctx, "hello"); err != nil {
		t.Fatalf("send: %v", err)
	}

	got := drainUntilDone(ctx, f.Events())
	var sawText, sawDone bool
	for _, et := range got {
		if et == EventText {
			sawText = true
		}
		if et == EventDone {
			sawDone = true
		}
	}
	if !sawText {
		t.Error("expected at least one text event")
	}
	if !sawDone {
		t.Error("expected done event")
	}
	if prompts := f.Prompts(); len(prompts) != 1 || prompts[0] != "hello" {
		t.Errorf("prompts=%v, want [hello]", prompts)
	}
	_ = f.Stop()
}

func TestFakeAdapter_CustomScript(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	script := []AgentEvent{
		E(EventText, "thinking..."),
		E(EventReasoning, "step 1"),
		E(EventToolCall, "", "tool", "Read", "id", "t1"),
		E(EventToolResult, "", "id", "t1", "is_error", false),
		E(EventFileChange, "", "path", "foo.go", "kind", "modify"),
		E(EventCost, "", "tokens_in", int64(100), "tokens_out", int64(20), "cost_usd", 0.001, "model", "fake"),
		E(EventDone, "", "reason", string(DoneTurnComplete)),
	}
	f := NewFakeAdapter(WithScripts([][]AgentEvent{script}), WithEventDelay(time.Millisecond))
	if err := f.Spawn(ctx, t.TempDir(), AgentConfig{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := f.SendPrompt(ctx, "go"); err != nil {
		t.Fatalf("send: %v", err)
	}

	want := []EventType{EventText, EventReasoning, EventToolCall, EventToolResult, EventFileChange, EventCost, EventDone}
	got := drainUntilDone(ctx, f.Events())
	if len(got) != len(want) {
		t.Fatalf("got %d events %v, want %d %v", len(got), got, len(want), want)
	}
	for i, et := range got {
		if et != want[i] {
			t.Errorf("event[%d]=%s, want %s", i, et, want[i])
		}
	}
	_ = f.Stop()
}

func TestFakeAdapter_SpawnError(t *testing.T) {
	f := NewFakeAdapter(WithSpawnError(errors.New("boom")))
	if err := f.Spawn(context.Background(), "", AgentConfig{}); err == nil || err.Error() != "boom" {
		t.Errorf("spawn err=%v, want boom", err)
	}
}

func TestFakeAdapter_NotSpawnedSend(t *testing.T) {
	f := NewFakeAdapter()
	if err := f.SendPrompt(context.Background(), "x"); err != ErrNotSpawned {
		t.Errorf("send err=%v, want ErrNotSpawned", err)
	}
}

func TestFakeAdapter_StopIsIdempotent(t *testing.T) {
	f := NewFakeAdapter()
	_ = f.Spawn(context.Background(), t.TempDir(), AgentConfig{})
	if err := f.Stop(); err != nil {
		t.Errorf("stop1: %v", err)
	}
	if err := f.Stop(); err != nil {
		t.Errorf("stop2: %v", err)
	}
}

func TestFakeAdapter_MultiPromptReplaysLastScript(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	script1 := []AgentEvent{E(EventText, "first"), E(EventDone, "", "reason", string(DoneTurnComplete))}
	script2 := []AgentEvent{E(EventText, "second"), E(EventDone, "", "reason", string(DoneTurnComplete))}
	f := NewFakeAdapter(WithScripts([][]AgentEvent{script1, script2}))
	_ = f.Spawn(ctx, t.TempDir(), AgentConfig{})
	defer f.Stop()

	_ = f.SendPrompt(ctx, "a")
	got := drainUntilDone(ctx, f.Events())
	if len(got) != 2 || got[0] != EventText {
		t.Errorf("prompt1 got=%v, want [text done]", got)
	}

	_ = f.SendPrompt(ctx, "b")
	got = drainUntilDone(ctx, f.Events())
	if len(got) != 2 || got[0] != EventText {
		t.Errorf("prompt2 got=%v, want [text done]", got)
	}

	_ = f.SendPrompt(ctx, "c")
	got = drainUntilDone(ctx, f.Events())
	if len(got) != 2 || got[0] != EventText {
		t.Errorf("prompt3 got=%v, want [text done] (replay last)", got)
	}

	if n := len(f.Prompts()); n != 3 {
		t.Errorf("prompts=%d, want 3", n)
	}
}

func TestMessageRole(t *testing.T) {
	cases := []struct {
		et   EventType
		want string
	}{
		{EventText, "assistant"},
		{EventReasoning, "assistant"},
		{EventToolCall, "tool"},
		{EventToolResult, "tool"},
		{EventFileChange, "tool"},
		{EventCost, "system"},
		{EventLag, "system"},
		{EventDone, "system"},
		{EventError, "system"},
		{EventApproval, "assistant"},
	}
	for _, c := range cases {
		if got := MessageRole(AgentEvent{Type: c.et}); got != c.want {
			t.Errorf("%s -> %q, want %q", c.et, got, c.want)
		}
	}
}

func TestCostOf(t *testing.T) {
	e := AgentEvent{
		Type:     EventCost,
		Metadata: MD("tokens_in", int64(150), "tokens_out", int64(40), "cost_usd", 0.0025, "model", "fake-1"),
	}
	c, ok := CostOf(e)
	if !ok {
		t.Fatal("expected ok")
	}
	if c.TokensIn != 150 || c.TokensOut != 40 || c.CostUSD != 0.0025 || c.Model != "fake-1" {
		t.Errorf("got %+v", c)
	}

	_, ok = CostOf(AgentEvent{Type: EventText})
	if ok {
		t.Error("expected not ok for non-cost event")
	}
}

func TestDoneReasonOf(t *testing.T) {
	cases := []struct {
		e    AgentEvent
		want DoneReason
	}{
		{AgentEvent{Type: EventDone, Metadata: MD("reason", string(DoneTurnComplete))}, DoneTurnComplete},
		{AgentEvent{Type: EventDone, Metadata: MD("reason", string(DoneError))}, DoneError},
		{AgentEvent{Type: EventDone}, DoneProcessExit},                             // missing -> default
		{AgentEvent{Type: EventDone, Metadata: MD("reason", 42)}, DoneProcessExit}, // wrong type -> default
		{AgentEvent{Type: EventText}, DoneProcessExit},                             // not done event
	}
	for _, c := range cases {
		if got := DoneReasonOf(c.e); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestFatalError(t *testing.T) {
	if got := FatalError(AgentEvent{Type: EventError, Content: "boom", Metadata: MD("fatal", true)}); got != "boom" {
		t.Errorf("got %q, want boom", got)
	}
	if got := FatalError(AgentEvent{Type: EventError, Content: "warn", Metadata: MD("fatal", false)}); got != "" {
		t.Errorf("non-fatal -> %q, want empty", got)
	}
	if got := FatalError(AgentEvent{Type: EventText, Content: "hi"}); got != "" {
		t.Errorf("non-error -> %q, want empty", got)
	}
}
