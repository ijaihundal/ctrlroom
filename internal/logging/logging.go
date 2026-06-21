package logging

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey struct{}

func New(env string) *slog.Logger {
	if env == "prod" {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func WithReqID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, reqID)
}

func ReqIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}

func FromCtx(ctx context.Context) *slog.Logger {
	l := slog.Default()
	if id := ReqIDFromCtx(ctx); id != "" {
		l = l.With(slog.String("req_id", id))
	}
	return l
}

func SetDefault(l *slog.Logger) {
	slog.SetDefault(l)
}
