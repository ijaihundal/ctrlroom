package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// openCodeAdapter speaks the OpenCode server HTTP/SSE protocol.
//
// Lifecycle:
//   - Spawn runs `opencode serve --port 0 --hostname 127.0.0.1` in the worktree
//     with a generated OPENCODE_SERVER_PASSWORD + OPENCODE_SERVER_USERNAME.
//     Waits for the "opencode server listening on http://..." line on stderr.
//   - After port discovery, POSTs /session to obtain a sessionID.
//   - SendPrompt POSTs /session/:id/prompt_async.
//   - A goroutine reads the /event SSE stream and translates events to AgentEvent.
//   - Stop POSTs /session/:id/abort and kills the process.
type openCodeAdapter struct {
	bin     string
	logger  *slog.Logger
	cwd     string
	cfg     AgentConfig
	cmd     *exec.Cmd
	baseURL string
	auth    string // "Basic <base64(user:pass)>"
	user    string
	pass    string

	sessionID string

	events          chan AgentEvent
	stopOnce        sync.Once
	stopCh          chan struct{}
	spawnCancel     context.CancelFunc
	mu              sync.Mutex
	turnDoneEmitted bool
}

func newOpenCodeAdapter(bin string) AgentAdapter {
	return &openCodeAdapter{
		bin:    bin,
		logger: slog.Default(),
		events: make(chan AgentEvent, 64),
		stopCh: make(chan struct{}),
	}
}

func (a *openCodeAdapter) Spawn(ctx context.Context, cwd string, cfg AgentConfig) error {
	a.cwd = cwd
	a.cfg = cfg

	a.user = "ctrlroom"
	a.pass = randHex(16)
	a.auth = "Basic " + basicAuth(a.user, a.pass)

	args := []string{
		"serve",
		"--port", "0",
		"--hostname", "127.0.0.1",
		"--print-logs",
	}
	// IMPORTANT: do NOT tie the subprocess to the Spawn ctx — that ctx is the
	// HTTP request context and will be cancelled when the handler returns,
	// killing opencode. Use a long-lived context that's only cancelled by
	// Stop() via a.spawnCancel.
	spawnCtx, spawnCancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(spawnCtx, a.bin, args...) //nolint:gosec // bin path is constructor-controlled
	cmd.Dir = cwd
	cmd.Env = append(envPreserveProviderKeys(), "OPENCODE_SERVER_USERNAME="+a.user, "OPENCODE_SERVER_PASSWORD="+a.pass)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(syscall.SIGTERM)
		}
		return os.ErrProcessDone
	}
	a.spawnCancel = spawnCancel

	stderr, err := cmd.StderrPipe()
	if err != nil {
		spawnCancel()
		return fmt.Errorf("stderr pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		spawnCancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		spawnCancel()
		return fmt.Errorf("%w: %s", ErrSpawnFailed, err)
	}
	a.cmd = cmd

	if err := a.waitForReady(ctx, cmd, stdout, stderr); err != nil {
		_ = cmd.Process.Kill()
		spawnCancel()
		return err
	}

	// Create session.
	sid, err := a.createSession(ctx)
	if err != nil {
		_ = cmd.Process.Kill()
		spawnCancel()
		return fmt.Errorf("create session: %w", err)
	}
	a.sessionID = sid
	slog.Info("opencode session ready", "session_id", sid, "base_url", a.baseURL)

	go a.readLoop()
	return nil
}

// waitForReady blocks until opencode prints its "listening on" line on stdout
// or stderr, or one of ctx / 30s deadline elapses.
func (a *openCodeAdapter) waitForReady(ctx context.Context, cmd *exec.Cmd, stdout, stderr io.Reader) error {
	listenRX := regexp.MustCompile(`listening on https?://([\d.]+):(\d+)`)
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()

	// readyDone closes once we've seen the listening line; afterwards the
	// drain goroutines just discard output to keep opencode's pipes from
	// backing up (which would kill the subprocess).
	readyDone := make(chan struct{})
	lineCh := make(chan string, 16)
	errCh := make(chan error, 2)

	scan := func(r io.Reader, name string) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 4096), 1024*1024)
		for sc.Scan() {
			select {
			case <-readyDone:
				// discard the line; just keep the pipe drained.
			case lineCh <- sc.Text():
			case <-a.stopCh:
				return
			}
		}
		if err := sc.Err(); err != nil {
			errCh <- fmt.Errorf("%s scan: %w", name, err)
		}
	}
	go scan(stdout, "stdout")
	go scan(stderr, "stderr")

	for {
		select {
		case line := <-lineCh:
			if m := listenRX.FindStringSubmatch(line); m != nil {
				a.baseURL = "http://127.0.0.1:" + m[2]
				close(readyDone)
				return nil
			}
		case err := <-errCh:
			return fmt.Errorf("wait for opencode ready: %w", err)
		case <-deadline.C:
			return fmt.Errorf("opencode serve did not become ready in 30s")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (a *openCodeAdapter) createSession(ctx context.Context) (string, error) {
	body := map[string]any{"title": "ctrlroom"}
	resp, err := a.post(ctx, "/session", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	if out.ID == "" {
		return "", errors.New("empty session id")
	}
	return out.ID, nil
}

func (a *openCodeAdapter) SendPrompt(ctx context.Context, prompt string) error {
	if a.sessionID == "" {
		return ErrNotSpawned
	}
	// Reset the per-turn done-emission flag.
	a.mu.Lock()
	a.turnDoneEmitted = false
	a.mu.Unlock()

	body := map[string]any{
		"parts": []map[string]any{
			{"type": "text", "text": prompt},
		},
	}
	path := "/session/" + a.sessionID + "/prompt_async"
	resp, err := a.post(ctx, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("prompt_async status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (a *openCodeAdapter) Events() <-chan AgentEvent { return a.events }

func (a *openCodeAdapter) RespondApproval(ctx context.Context, id string, approved bool, feedback string) error {
	return nil // autonomous mode — approvals not surfaced
}

func (a *openCodeAdapter) Stop() error {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		// Abort the active turn if any.
		if a.sessionID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if resp, err := a.post(ctx, "/session/"+a.sessionID+"/abort", nil); err == nil {
				_ = resp.Body.Close()
			}
		}
		// Cancel the spawn context (triggers SIGTERM via cmd.Cancel), then
		// force kill if it doesn't exit.
		if a.spawnCancel != nil {
			a.spawnCancel()
		}
		if a.cmd != nil && a.cmd.Process != nil {
			_ = a.cmd.Process.Kill()
		}
		close(a.events)
	})
	return nil
}

// readLoop subscribes to /event and translates the SSE stream to AgentEvent.
// One global stream; we filter by sessionID.
func (a *openCodeAdapter) readLoop() {
	defer func() {
		// Recovery safety: never close the channel twice (Stop also closes).
	}()
	for {
		select {
		case <-a.stopCh:
			return
		default:
		}
		if err := a.connectAndStream(); err != nil {
			// On error, sleep briefly and retry until stopped.
			select {
			case <-a.stopCh:
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

func (a *openCodeAdapter) connectAndStream() error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, a.baseURL+"/event", http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", a.auth)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("event stream status %d", resp.StatusCode)
	}

	// SSE parser: events are separated by blank lines. Each event is a series
	// of "field: value" lines. We only care about "data:".
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 4*1024*1024)
	var dataBuf bytes.Buffer
	for sc.Scan() {
		select {
		case <-a.stopCh:
			return nil
		default:
		}
		line := sc.Text()
		if line == "" {
			// End of event.
			if dataBuf.Len() > 0 {
				a.handleSSEEvent(dataBuf.Bytes())
				dataBuf.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataBuf.WriteString(strings.TrimPrefix(line, "data:"))
			dataBuf.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// handleSSEEvent parses the JSON payload from a single SSE event and emits
// zero or more AgentEvents into the adapter's events channel.
//
// Filters by sessionID: only events belonging to a.sessionID are surfaced.
// Connection-level events (server.connected, heartbeat, etc.) are dropped.
func (a *openCodeAdapter) handleSSEEvent(payload []byte) {
	// Compact newlines between data lines into the JSON payload.
	compact := bytes.TrimSpace(payload)
	if len(compact) == 0 {
		return
	}
	var ev struct {
		ID         string          `json:"id"`
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(compact, &ev); err != nil {
		return
	}

	// Filter by sessionID where present.
	var sid string
	_ = json.Unmarshal(ev.Properties, &struct {
		SessionID *string `json:"sessionID"`
	}{SessionID: &sid})
	if sid != "" && sid != a.sessionID {
		return
	}

	switch ev.Type {
	case "message.part.delta":
		a.handlePartDelta(ev.Properties)
	case "message.part.updated":
		a.handlePartUpdated(ev.Properties)
	case "message.updated":
		a.handleMessageUpdated(ev.Properties)
	}
}

func (a *openCodeAdapter) handlePartDelta(props json.RawMessage) {
	var d struct {
		Field string `json:"field"`
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(props, &d); err != nil {
		return
	}
	switch d.Field {
	case "text", "":
		a.emit(AgentEvent{Type: EventText, Content: d.Delta})
	case "reasoning":
		a.emit(AgentEvent{Type: EventReasoning, Content: d.Delta})
	}
}

func (a *openCodeAdapter) handlePartUpdated(props json.RawMessage) {
	var d struct {
		Part struct {
			Type   string          `json:"type"`
			Text   string          `json:"text"`
			Tokens json.RawMessage `json:"tokens"`
			Cost   *float64        `json:"cost"`
			Reason string          `json:"reason"`
		} `json:"part"`
	}
	if err := json.Unmarshal(props, &d); err != nil {
		return
	}
	if d.Part.Type == "step-finish" {
		// Map tokens + cost to EventCost.
		meta := map[string]any{}
		if len(d.Part.Tokens) > 0 {
			var t struct {
				Input  int64 `json:"input"`
				Output int64 `json:"output"`
			}
			_ = json.Unmarshal(d.Part.Tokens, &t)
			meta["tokens_in"] = t.Input
			meta["tokens_out"] = t.Output
		}
		if d.Part.Cost != nil {
			meta["cost_usd"] = *d.Part.Cost
		}
		meta["model"] = a.modelLabel()
		a.emit(AgentEvent{Type: EventCost, Metadata: meta})
	}
}

func (a *openCodeAdapter) handleMessageUpdated(props json.RawMessage) {
	var d struct {
		Info struct {
			Role   string `json:"role"`
			Finish string `json:"finish"`
		} `json:"info"`
	}
	if err := json.Unmarshal(props, &d); err != nil {
		return
	}
	// Only end the turn when an assistant message finishes with "stop".
	// Dedupe: opencode can re-emit the final message.updated with timing data
	// appended; only emit EventDone once per turn.
	if d.Info.Role == "assistant" && d.Info.Finish == "stop" {
		a.mu.Lock()
		prev := a.turnDoneEmitted
		a.turnDoneEmitted = true
		a.mu.Unlock()
		if !prev {
			a.emit(AgentEvent{Type: EventDone, Metadata: map[string]any{"reason": string(DoneTurnComplete)}})
		}
	}
}

func (a *openCodeAdapter) emit(ev AgentEvent) {
	select {
	case a.events <- ev:
	case <-a.stopCh:
	}
}

func (a *openCodeAdapter) modelLabel() string {
	if a.cfg.Model != "" {
		return a.cfg.Model
	}
	return "opencode"
}

// --- HTTP helpers ---

func (a *openCodeAdapter) post(ctx context.Context, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", a.auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

// --- utilities ---

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// envPreserveProviderKeys returns a minimal env containing only provider
// credentials + PATH + HOME, suitable for spawning an isolated opencode serve
// subprocess. We intentionally exclude OPENCODE* env vars from the parent so
// the child doesn't think it's already part of an opencode process tree.
func envPreserveProviderKeys() []string {
	keep := make([]string, 0, 64)
	for _, kv := range os.Environ() {
		key := strings.SplitN(kv, "=", 2)[0]
		// Drop OpenCode-internal vars but keep provider keys (anything ending
		// in _API_KEY, _TOKEN, etc.).
		if strings.HasPrefix(key, "OPENCODE") {
			continue
		}
		keep = append(keep, kv)
	}
	return keep
}
