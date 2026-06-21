package agent

import (
	"context"
	"errors"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

type EventType string

const (
	EventText       EventType = "text"
	EventReasoning  EventType = "reasoning"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventFileChange EventType = "file_change"
	EventCost       EventType = "cost"
	EventApproval   EventType = "approval_request"
	EventLag        EventType = "lag"
	EventError      EventType = "error"
	EventDone       EventType = "done"
)

type DoneReason string

const (
	DoneTurnComplete DoneReason = "turn_complete"
	DoneStopped      DoneReason = "stopped"
	DoneError        DoneReason = "error"
	DoneProcessExit  DoneReason = "process_exit"
)

type AgentEvent struct {
	Type     EventType      `json:"type"`
	Content  string         `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type AgentConfig struct {
	APIKey    string // provider API key (may be empty if the binary uses its own config)
	Model     string // optional model override (e.g., "zai/glm-5.2"); empty = agent default
	AgentType types.AgentType
	ExtraEnv  map[string]string // additional env vars passed to the subprocess
}

// AgentAdapter abstracts a coding-agent subprocess.
//
// Lifecycle invariants:
//   - Spawn is synchronous: returns only after the protocol handshake completes
//     (OpenCode POST /session 2xx; Claude system/init; Codex initialize ack).
//     Errors here are fatal to the workspace.
//   - Events() returns a single buffered channel (cap 64) for the adapter's
//     lifetime. The adapter must never block on a full channel — on overflow,
//     drop the oldest non-terminal event and emit an EventLag with
//     metadata={"dropped": N}.
//   - EventDone is emitted per turn (one per SendPrompt response cycle). It
//     does NOT close the channel — the adapter stays alive for follow-up
//     SendPrompt calls. The Manager transitions the workspace to
//     awaiting_input on EventDone{turn_complete} and keeps draining.
//   - The Events() channel is closed only when the adapter is terminating:
//     Stop() was called, the subprocess exited, or an unrecoverable error
//     occurred. After closure, no further events will be emitted on this
//     adapter instance.
//   - Stop() is idempotent and closes the Events() channel.
type AgentAdapter interface {
	Spawn(ctx context.Context, cwd string, cfg AgentConfig) error
	SendPrompt(ctx context.Context, prompt string) error
	Events() <-chan AgentEvent
	RespondApproval(ctx context.Context, id string, approved bool, feedback string) error
	Stop() error
}

// Errors
var (
	ErrNotImplemented   = errors.New("agent adapter not implemented for this type")
	ErrBinaryMissing    = errors.New("agent binary not found in PATH")
	ErrAlreadySpawned   = errors.New("adapter already spawned")
	ErrNotSpawned       = errors.New("adapter not spawned")
	ErrSpawnFailed      = errors.New("adapter spawn failed")
	ErrPromptDuringTurn = errors.New("a turn is already in flight; queue follow-ups")
)
