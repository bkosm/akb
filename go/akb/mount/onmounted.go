package mount

import (
	"context"
	"fmt"
)

// OnMounted is called after a KB is successfully mounted. It starts any
// post-mount processing (e.g. file watching) and returns a stop func that
// tears it down, plus any error. A nil stop func is valid.
type OnMounted func(kbName, mountPath string) (func(), error)

type onMountedKey struct{}

// OnMountedIntoContext stores an OnMounted hook in the context.
func OnMountedIntoContext(ctx context.Context, fn OnMounted) context.Context {
	return context.WithValue(ctx, onMountedKey{}, fn)
}

// OnMountedFromContext retrieves the OnMounted hook stored by
// OnMountedIntoContext. Returns an error if none was set.
func OnMountedFromContext(ctx context.Context) (OnMounted, error) {
	v := ctx.Value(onMountedKey{})
	if v == nil {
		return nil, fmt.Errorf("no OnMounted hook in context")
	}
	fn, ok := v.(OnMounted)
	if !ok || fn == nil {
		return nil, fmt.Errorf("no OnMounted hook in context")
	}
	return fn, nil
}
