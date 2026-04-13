package mount

import (
	"context"
	"log/slog"

	"github.com/bkosm/akb/go/akb/config"
)

// ServeSetup prepares the manager for serving a set of KBs. It runs
// preflight for any remote KB, then returns two lifecycle functions:
//
//	run     — call in a goroutine after the MCP server starts; mounts every
//	          KB by calling Add(ctx, ...). If an OnMounted hook is stored in
//	          ctx, Add invokes it and tracks the returned stop func.
//	cleanup — call on shutdown; invokes all tracked stop funcs, then unmounts
//	          all remote KBs.
//
// err is non-nil only when preflight fails (e.g. rclone not on PATH).
func (m *Manager) ServeSetup(
	ctx context.Context,
	kbs map[config.Unique]config.KB,
) (run func(), cleanup func(), err error) {
	for _, kb := range kbs {
		if kb.RcloneRemote != "" {
			if err := m.Preflight(); err != nil {
				return nil, nil, err
			}
			break
		}
	}

	run = func() {
		for name, kb := range kbs {
			if err := m.Add(ctx, string(name), kb.RcloneRemote, kb.Mount, Method(kb.Method), kb.RcloneArgs); err != nil {
				slog.Error("mount kb", "kb", name, "err", err)
			}
		}
	}

	cleanup = func() {
		if err := m.unmountAll(); err != nil {
			slog.Error("cleanup unmount", "err", err)
		}
	}

	return run, cleanup, nil
}
