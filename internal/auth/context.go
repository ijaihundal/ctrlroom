package auth

import (
	"context"
	"fmt"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

type ctxKey struct{}

func WithUser(ctx context.Context, u *types.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

func UserFromCtx(ctx context.Context) *types.User {
	v, _ := ctx.Value(ctxKey{}).(*types.User)
	return v
}

func RequireUser(ctx context.Context) (*types.User, error) {
	u := UserFromCtx(ctx)
	if u == nil {
		return nil, fmt.Errorf("no authenticated user in context")
	}
	return u, nil
}
