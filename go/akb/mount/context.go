package mount

import (
	"context"
	"fmt"
)

type ctxKey struct{}

// IntoContext stores the mount Manager into the context.
func IntoContext(ctx context.Context, mgr *Manager) context.Context {
	return context.WithValue(ctx, ctxKey{}, mgr)
}

// FromContext retrieves the mount Manager stored by IntoContext.
// Returns an error if no manager was set.
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
