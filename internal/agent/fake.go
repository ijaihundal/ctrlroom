package agent

import (
	"context"
	"sync"
	"time"
)

// FakeAdapter replays a caller-supplied script of events for each SendPrompt
// call. Designed for table-driven tests of the Manager / persistence layer.
type FakeAdapter struct {
	mu          sync.Mutex
	scripts     [][]AgentEvent // one script per SendPrompt; cycles if more calls arrive
	delay       time.Duration  // delay between events within a script
	prompts     []string       // record of prompts received, in order
	events      chan AgentEvent
	spawned     bool
	spawnErr    error
	sendErr     error
	stopped     bool
	spawnDelay  time.Duration
	spawnCloses chan struct{} // closes when Spawn is called; for synchronously tests
	stopSignal  chan struct{} // closed when Stop is called; signals replay to bail
	cwd         string
	cfg         AgentConfig
}

// FakeOption configures a FakeAdapter.
type FakeOption func(*FakeAdapter)

func WithScripts(scripts [][]AgentEvent) FakeOption {
	return func(f *FakeAdapter) { f.scripts = scripts }
}

func WithEventDelay(d time.Duration) FakeOption {
	return func(f *FakeAdapter) { f.delay = d }
}

func WithSpawnError(err error) FakeOption {
	return func(f *FakeAdapter) { f.spawnErr = err }
}

func WithSendError(err error) FakeOption {
	return func(f *FakeAdapter) { f.sendErr = err }
}

func WithSpawnDelay(d time.Duration) FakeOption {
	return func(f *FakeAdapter) { f.spawnDelay = d }
}

func NewFakeAdapter(opts ...FakeOption) *FakeAdapter {
	f := &FakeAdapter{
		events:      make(chan AgentEvent, 64),
		spawnCloses: make(chan struct{}),
		stopSignal:  make(chan struct{}),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (f *FakeAdapter) Spawn(ctx context.Context, cwd string, cfg AgentConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spawned {
		return ErrAlreadySpawned
	}
	if f.spawnErr != nil {
		close(f.spawnCloses)
		return f.spawnErr
	}
	if f.spawnDelay > 0 {
		select {
		case <-time.After(f.spawnDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.spawned = true
	f.cwd = cwd
	f.cfg = cfg
	close(f.spawnCloses)
	return nil
}

func (f *FakeAdapter) SendPrompt(ctx context.Context, prompt string) error {
	f.mu.Lock()
	if !f.spawned {
		f.mu.Unlock()
		return ErrNotSpawned
	}
	if f.sendErr != nil {
		f.mu.Unlock()
		return f.sendErr
	}
	if f.stopped {
		f.mu.Unlock()
		return ErrNotSpawned
	}
	f.prompts = append(f.prompts, prompt)
	script := f.nextScriptLocked()
	events := f.events
	stopCh := f.stopSignal
	f.mu.Unlock()

	go f.replay(script, events, ctx, stopCh)
	return nil
}

func (f *FakeAdapter) nextScriptLocked() []AgentEvent {
	if len(f.scripts) == 0 {
		// Default: just emit a single text event + done.
		return []AgentEvent{
			{Type: EventText, Content: "(fake adapter has no script; emitting default)"},
			{Type: EventDone, Metadata: map[string]any{"reason": string(DoneTurnComplete)}},
		}
	}
	idx := len(f.prompts) - 1
	if idx >= len(f.scripts) {
		idx = len(f.scripts) - 1 // last script replays for additional calls
	}
	return f.scripts[idx]
}

func (f *FakeAdapter) replay(script []AgentEvent, events chan AgentEvent, ctx context.Context, stopCh <-chan struct{}) {
	// Do NOT close the channel here — the adapter is multi-turn. Only Stop()
	// closes the events channel (per the AgentAdapter contract).
	for _, e := range script {
		if f.delay > 0 {
			select {
			case <-time.After(f.delay):
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			}
		}
		// Send under the lock so Stop can't close the channel mid-send.
		// Non-blocking per the contract: drop on overflow rather than block.
		f.mu.Lock()
		stopped := f.stopped
		if !stopped {
			select {
			case events <- e:
			case <-stopCh:
				stopped = true
			default:
				// drop on overflow
			}
		}
		f.mu.Unlock()
		if stopped {
			return
		}
	}
}

func (f *FakeAdapter) Events() <-chan AgentEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events
}

func (f *FakeAdapter) RespondApproval(ctx context.Context, id string, approved bool, feedback string) error {
	return nil // fake doesn't model approvals
}

func (f *FakeAdapter) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return nil
	}
	f.stopped = true
	// Signal any in-flight replay goroutine to bail, then close the events
	// channel. Per the contract, the channel closes only at adapter termination.
	close(f.stopSignal)
	close(f.events)
	return nil
}

// Prompts returns the prompts received so far, in order. Test-only helper.
func (f *FakeAdapter) Prompts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.prompts))
	copy(out, f.prompts)
	return out
}

// Cwd returns the cwd passed to Spawn. Test-only helper.
func (f *FakeAdapter) Cwd() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cwd
}

// Cfg returns the cfg passed to Spawn. Test-only helper.
func (f *FakeAdapter) Cfg() AgentConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg
}

// WaitSpawned blocks until Spawn has been called or ctx is canceled.
func (f *FakeAdapter) WaitSpawned(ctx context.Context) error {
	select {
	case <-f.spawnCloses:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Ensure FakeAdapter satisfies the interface.
var _ AgentAdapter = (*FakeAdapter)(nil)
