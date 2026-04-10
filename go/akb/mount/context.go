package mount

import (
	"context"
	"fmt"
)

type ctxKey struct{}

func IntoContext(ctx context.Context, mgr *Manager) context.Context {
	return context.WithValue(ctx, ctxKey{}, mgr)
}

func FromContext(ctx context.Context) (*Manager, error) {
	v := ctx.Value(ctxKey{})
	if v == nil {
		return nil, fmt.Errorf("mount manager not initialized in context")
	}
	mgr, ok := v.(*Manager)
	if !ok || mgr == nil {
		return nil, fmt.Errorf("mount manager not initialized in context")
	}
	return mgr, nil
}
