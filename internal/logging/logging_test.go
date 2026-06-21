package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestReqIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := ReqIDFromCtx(ctx); got != "" {
		t.Errorf("ReqIDFromCtx(empty ctx) = %q, want empty", got)
	}

	ctx = WithReqID(ctx, "req-abc")
	if got := ReqIDFromCtx(ctx); got != "req-abc" {
		t.Errorf("ReqIDFromCtx = %q, want %q", got, "req-abc")
	}
}

func TestFromCtxReturnsDefault(t *testing.T) {
	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, nil))
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	SetDefault(custom)

	l := FromCtx(context.Background())
	if l == nil {
		t.Fatal("FromCtx returned nil logger")
	}
	l.Info("probe-message")
	if !strings.Contains(buf.String(), "probe-message") {
		t.Errorf("default logger not used; output = %q", buf.String())
	}
}

func TestFromCtxAttachesReqID(t *testing.T) {
	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, nil))
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	SetDefault(custom)

	ctx := WithReqID(context.Background(), "rid-xyz")
	FromCtx(ctx).Info("tagged")
	if !strings.Contains(buf.String(), "req_id=rid-xyz") {
		t.Errorf("expected req_id attribute in output, got %q", buf.String())
	}
}

func TestNewDev(t *testing.T) {
	l := New("dev")
	if l == nil {
		t.Fatal("New(dev) returned nil")
	}
}

func TestNewProd(t *testing.T) {
	l := New("prod")
	if l == nil {
		t.Fatal("New(prod) returned nil")
	}
}
