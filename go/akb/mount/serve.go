package mount

import (
	"log/slog"
	"sync"

	"github.com/bkosm/akb/go/akb/config"
)

// ServeSetup prepares the manager for serving a set of KBs. It runs
// preflight for any remote KB, then returns two lifecycle functions:
//
//	run     — call in a goroutine after the MCP server starts; mounts every
//	          KB and, on success, calls onMounted(name, mountPath). If
//	          onMounted returns a non-nil stop func it is collected for
//	          cleanup.
//	cleanup — call on shutdown; invokes every collected stop func, then
//	          unmounts all remote KBs.
//
// err is non-nil only when preflight fails (e.g. rclone not on PATH).
//
// onMounted may be nil if no post-mount action is required.
func (m *Manager) ServeSetup(
	kbs map[config.Unique]config.KB,
	onMounted func(name, mountPath string) func(),
) (run func(), cleanup func(), err error) {
	for _, kb := range kbs {
		if kb.RcloneRemote != "" {
			if err := m.Preflight(); err != nil {
				return nil, nil, err
			}
			break
		}
	}

	var (
		mu    sync.Mutex
		stops []func()
	)

	run = func() {
		for name, kb := range kbs {
			if err := m.Add(kb.RcloneRemote, kb.Mount, MountMethod(kb.MountMethod), kb.RcloneArgs); err != nil {
				slog.Error("mount kb", "kb", name, "err", err)
				continue
			}
			if onMounted == nil {
				continue
			}
			if stop := onMounted(string(name), kb.Mount); stop != nil {
				mu.Lock()
				stops = append(stops, stop)
				mu.Unlock()
			}
		}
	}

	cleanup = func() {
		mu.Lock()
		for _, stop := range stops {
			stop()
		}
		mu.Unlock()
		if err := m.unmountAll(); err != nil {
			slog.Error("cleanup unmount", "err", err)
		}
	}

	return run, cleanup, nil
}
