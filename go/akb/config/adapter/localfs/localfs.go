// Package localfs provides a local filesystem implementation of config.Interface.
// Config is stored as a JSON file at a configurable path. Env vars in the path
// are expanded at access time. There is no locking; concurrent writes from
// multiple processes use last-writer-wins semantics.
package localfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bkosm/akb/config"
)

// LocalFS is a config.Interface backed by a JSON file on disk.
// Path supports environment variable expansion (e.g. $HOME/.config/akb/config.json).
type LocalFS struct {
	Path string
}

func (l *LocalFS) resolvedPath() string {
	return os.ExpandEnv(l.Path)
}

func (l *LocalFS) Retrieve(ctx context.Context) (config.Config, error) {
	path := l.resolvedPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return config.Config{}, fmt.Errorf("read config file %q: %w", path, err)
		}
		empty := config.Config{
			KBs: make(map[config.Unique]config.KB),
		}
		if err := l.Save(ctx, empty); err != nil {
			return config.Config{}, fmt.Errorf("init config file %q: %w", path, err)
		}
		return empty, nil
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, fmt.Errorf("parse config file %q: %w", path, err)
	}
	return cfg, nil
}

func (l *LocalFS) Save(_ context.Context, c config.Config) error {
	path := l.resolvedPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir for %q: %w", path, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config file %q: %w", path, err)
	}
	return nil
}

var _ config.Interface = &LocalFS{}
