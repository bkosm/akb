package config

import (
	"context"
	"errors"
	"fmt"
)

// ErrConflict is returned by Save when the underlying store detects that the
// config was modified by another writer since the last Retrieve. Callers should
// re-retrieve and retry.
var ErrConflict = errors.New("config was modified by another writer; re-retrieve and retry")

// Unique is the stable identifier for a knowledge base within the config.
type Unique string

// KB holds the configuration for a single knowledge base entry.
type KB struct {
	RcloneRemote string            `json:"rclone_remote,omitempty"`
	Mount        string            `json:"mount"`
	Method       string            `json:"mount_method,omitempty"` // "fuse", "nfs", or "" (auto: prefer FUSE, fall back to NFS)
	RcloneArgs   map[string]string `json:"rclone_args,omitempty"`  // flag overrides keyed by flag name without "--"; empty value for boolean flags
	Description  string            `json:"description,omitempty"`
}

// Config is the top-level configuration structure persisted by the config backend.
type Config struct {
	KBs map[Unique]KB `json:"kbs"`
}

// Interface is the storage abstraction for reading and writing AKB configuration.
// Implementations must be safe for concurrent Retrieve calls. Save uses
// optimistic locking where supported; see ErrConflict.
type Interface interface {
	Retrieve(context.Context) (Config, error)
	Save(context.Context, Config) error
}

type ctxKey struct{}

// IntoContext stores a config.Interface implementation into the context.
func IntoContext(ctx context.Context, cfg Interface) context.Context {
	return context.WithValue(ctx, ctxKey{}, cfg)
}

// FromContext retrieves the config.Interface stored by IntoContext.
// Returns an error if no config was set.
func FromContext(ctx context.Context) (Interface, error) {
	v := ctx.Value(ctxKey{})
	if v == nil {
		return nil, fmt.Errorf("config not initialized in context")
	}
	cfg, ok := v.(Interface)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("config not initialized in context")
	}
	return cfg, nil
}
