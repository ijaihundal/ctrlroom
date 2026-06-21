package agent

import (
	"context"
	"errors"
	"os/exec"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// AdapterFactory returns an AgentAdapter for the given agent type.
// Implementations decide how to discover binaries, build configs, etc.
type AdapterFactory func(t types.AgentType) (AgentAdapter, error)

// Binaries maps each agent type to the binary name (or path) used to spawn it.
// Empty string means "not configured" — Spawn will fail.
type Binaries map[types.AgentType]string

// NewAdapterFactory returns a factory that constructs adapters per agent type.
// OpenCode gets a real adapter (opencode.go); Claude/Codex return
// ErrNotImplemented (deferred per DESIGN D16). Missing binaries return an
// adapter whose Spawn fails with ErrBinaryMissing + MissingBinaryError.
func NewAdapterFactory(binaries Binaries) AdapterFactory {
	return func(t types.AgentType) (AgentAdapter, error) {
		switch t {
		case types.AgentOpenCode:
			bin := binaries[t]
			if bin == "" {
				bin = "opencode"
			}
			if _, err := exec.LookPath(bin); err != nil {
				//nolint:nilerr // intentional: we return an adapter that will fail on Spawn
				// with a clear error rather than failing factory construction.
				return &missingBinaryAdapter{name: bin}, nil
			}
			return newOpenCodeAdapter(bin), nil
		case types.AgentClaude, types.AgentCodex:
			return &unimplementedAdapter{t: t}, nil
		}
		return &unimplementedAdapter{t: t}, nil
	}
}

// AgentsPresent reports which agent types have a discoverable binary.
// Computed once at startup; used by the /health endpoint.
func AgentsPresent(binaries Binaries) map[types.AgentType]bool {
	out := make(map[types.AgentType]bool, len(binaries))
	for t, bin := range binaries {
		if bin == "" {
			continue
		}
		if _, err := exec.LookPath(bin); err == nil {
			out[t] = true
		}
	}
	return out
}

// missingBinaryAdapter satisfies the interface but Spawn always fails with
// ErrBinaryMissing. Lets us construct an adapter for a workspace even when the
// binary isn't installed; the workspace will go to status=failed with a clear
// message when Start is attempted.
type missingBinaryAdapter struct {
	name string
}

func (a *missingBinaryAdapter) Spawn(ctx context.Context, cwd string, cfg AgentConfig) error {
	return errors.Join(ErrBinaryMissing, &MissingBinaryError{Name: a.name})
}
func (a *missingBinaryAdapter) SendPrompt(ctx context.Context, prompt string) error {
	return ErrNotSpawned
}
func (a *missingBinaryAdapter) Events() <-chan AgentEvent {
	return make(chan AgentEvent) // never emits
}
func (a *missingBinaryAdapter) RespondApproval(ctx context.Context, id string, approved bool, feedback string) error {
	return ErrNotSpawned
}
func (a *missingBinaryAdapter) Stop() error { return nil }

// unimplementedAdapter is returned for agent types we haven't built adapters
// for yet (Claude and Codex, per DESIGN D16).
type unimplementedAdapter struct {
	t types.AgentType
}

func (a *unimplementedAdapter) Spawn(ctx context.Context, cwd string, cfg AgentConfig) error {
	return errors.Join(ErrNotImplemented, &UnimplementedError{Type: a.t})
}
func (a *unimplementedAdapter) SendPrompt(ctx context.Context, prompt string) error {
	return ErrNotSpawned
}
func (a *unimplementedAdapter) Events() <-chan AgentEvent {
	return make(chan AgentEvent) // never emits
}
func (a *unimplementedAdapter) RespondApproval(ctx context.Context, id string, approved bool, feedback string) error {
	return ErrNotSpawned
}
func (a *unimplementedAdapter) Stop() error { return nil }

// MissingBinaryError carries the binary name that couldn't be found.
type MissingBinaryError struct{ Name string }

func (e *MissingBinaryError) Error() string { return "agent binary not found: " + e.Name }

// UnimplementedError carries the agent type that has no adapter yet.
type UnimplementedError struct{ Type types.AgentType }

func (e *UnimplementedError) Error() string {
	return "agent adapter not implemented: " + string(e.Type)
}
