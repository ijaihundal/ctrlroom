package agent

import "github.com/ijaihundal/ctrlroom/internal/types"

// MessageRole maps an AgentEvent to the role of the messages table row it should
// be persisted as. Returns "" for events that should not be persisted as messages
// (none currently, but reserved).
func MessageRole(e AgentEvent) string {
	switch e.Type {
	case EventText, EventReasoning:
		return "assistant"
	case EventToolCall, EventToolResult, EventFileChange:
		return "tool"
	case EventApproval:
		return "assistant"
	case EventCost, EventLag, EventDone, EventError:
		return "system"
	}
	return "system"
}

// IsTerminal reports whether the event ends an adapter's stream.
// True for EventDone only (the only terminal event per the contract).
func IsTerminal(e AgentEvent) bool {
	return e.Type == EventDone
}

// FatalError returns the error message if e is a fatal error event, else "".
func FatalError(e AgentEvent) string {
	if e.Type == EventError {
		if v, ok := e.Metadata["fatal"].(bool); ok && v {
			return e.Content
		}
	}
	return ""
}

// DoneReasonOf extracts the DoneReason from an EventDone, defaulting to
// DoneProcessExit if missing or malformed.
func DoneReasonOf(e AgentEvent) DoneReason {
	if e.Type != EventDone {
		return DoneProcessExit
	}
	if v, ok := e.Metadata["reason"].(string); ok {
		return DoneReason(v)
	}
	return DoneProcessExit
}

// IsUserFacing reports whether the event should be displayed prominently in a
// chat-style UI (text, reasoning, tool calls, file changes, errors). Hidden
// events (cost, lag, done, approval) are bookkeeping.
func IsUserFacing(e AgentEvent) bool {
	switch e.Type {
	case EventText, EventReasoning, EventToolCall, EventToolResult, EventFileChange, EventError:
		return true
	}
	return false
}

// CostOf extracts token/cost fields from an EventCost. Returns ok=false if the
// event isn't a cost event or the fields are missing/wrong-typed.
type Cost struct {
	TokensIn  int64
	TokensOut int64
	CostUSD   float64
	Model     string
}

func CostOf(e AgentEvent) (Cost, bool) {
	if e.Type != EventCost {
		return Cost{}, false
	}
	var c Cost
	c.TokensIn = int64FromAny(e.Metadata["tokens_in"])
	c.TokensOut = int64FromAny(e.Metadata["tokens_out"])
	if v, ok := e.Metadata["cost_usd"].(float64); ok {
		c.CostUSD = v
	}
	if v, ok := e.Metadata["model"].(string); ok {
		c.Model = v
	}
	return c, true
}

func int64FromAny(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	}
	return 0
}

// Verify at compile time that types.AgentType constants exist where we expect.
var _ = []types.AgentType{
	types.AgentClaude,
	types.AgentCodex,
	types.AgentOpenCode,
}
