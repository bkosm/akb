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

type Unique string

type KB struct {
	RcloneRemote string            `json:"rclone_remote,omitempty"`
	Mount        string            `json:"mount"`
	MountMethod  string            `json:"mount_method,omitempty"` // "fuse", "nfs", or "" (auto: prefer FUSE, fall back to NFS)
	RcloneArgs   map[string]string `json:"rclone_args,omitempty"`  // flag overrides keyed by flag name without "--"; empty value for boolean flags
	Description  string            `json:"description,omitempty"`
}

type Config struct {
	KBs map[Unique]KB `json:"kbs"`
}

type Interface interface {
	Retrieve(context.Context) (Config, error)
	Save(context.Context, Config) error
}

type ctxKey struct{}

func IntoContext(ctx context.Context, cfg Interface) context.Context {
	return context.WithValue(ctx, ctxKey{}, cfg)
}

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
