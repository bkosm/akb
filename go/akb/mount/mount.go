package mount

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
)

// DefaultRcloneArgs are the baseline flags applied to every rclone mount/nfsmount
// invocation. Per-KB rclone_args override these; keys are flag names without "--".
// Empty value means a boolean flag.
var DefaultRcloneArgs = map[string]string{
	"vfs-cache-mode":     "full",
	"vfs-cache-max-size": "1G",
	"vfs-cache-max-age":  "48h",
	"dir-cache-time":     "30s",
	"poll-interval":      "15s",
	"vfs-write-back":     "5s",
}

// Method determines how remote storage is mounted locally.
type Method string

const (
	MethodAuto Method = ""     // prefer FUSE, fall back to NFS
	MethodFuse Method = "fuse" // rclone mount (requires FUSE library)
	MethodNFS  Method = "nfs"  // rclone nfsmount (no FUSE dependency)
)

type entry struct {
	remote     string // empty for plain local directories
	mountpoint string
	method     Method    // resolved method (never MethodAuto)
	cmd        *exec.Cmd // rclone child process (nil for local dirs)
}

// Manager tracks rclone mounts and plain local directories.
type Manager struct {
	mu              sync.Mutex
	entries         map[string]*entry // mountpoint -> entry
	stops           map[string]func() // mountpoint -> watcher stop func
	mountErrs       map[string]error  // mountpoint -> last Add error (nil = no error)
	fuseUnmountBin  string            // "fusermount3"/"fusermount" (Linux only, for FUSE unmounts)
	hasFUSE         bool
	preflightCalled bool
}

func NewManager() *Manager {
	return &Manager{
		entries:   make(map[string]*entry),
		stops:     make(map[string]func()),
		mountErrs: make(map[string]error),
	}
}

// MountError returns the error from the last failed Add for the given mountpoint,
// or nil if the last Add succeeded or the mountpoint has never been attempted.
func (m *Manager) MountError(mountpoint string) error {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mountErrs[mountpoint]
}

// SetMountError stores or clears a mount error for the given mountpoint.
// Pass err=nil to clear. Intended for testing.
func (m *Manager) SetMountError(mountpoint string, err error) {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.mountErrs, mountpoint)
	} else {
		m.mountErrs[mountpoint] = err
	}
}

func (m *Manager) recordMountErr(mountpoint string, err error) {
	m.mu.Lock()
	if err != nil {
		m.mountErrs[mountpoint] = err
	} else {
		delete(m.mountErrs, mountpoint)
	}
	m.mu.Unlock()
}

// Preflight checks that rclone is available and probes FUSE support.
// Call once at startup before any Add calls.
func (m *Manager) Preflight() error {
	if _, err := exec.LookPath("rclone"); err != nil {
		return fmt.Errorf("rclone not found: download from https://rclone.org/downloads/")
	}

	switch runtime.GOOS {
	case "darwin":
		// cgofuse probes these dylibs in order: macFUSE, osxfuse, FUSE-T.
		// Also check the .fs bundles used by older installers.
		m.hasFUSE = pathExists("/usr/local/lib/libfuse.2.dylib") ||
			pathExists("/usr/local/lib/libosxfuse.2.dylib") ||
			pathExists("/usr/local/lib/libfuse-t.dylib") ||
			pathExists("/Library/Filesystems/macfuse.fs") ||
			pathExists("/Library/Filesystems/fuse-t.fs")

	case "linux":
		if pathExists("/dev/fuse") {
			if bin, err := exec.LookPath("fusermount3"); err == nil {
				m.fuseUnmountBin = bin
				m.hasFUSE = true
			} else if bin, err := exec.LookPath("fusermount"); err == nil {
				m.fuseUnmountBin = bin
				m.hasFUSE = true
			}
		}

	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	m.preflightCalled = true
	return nil
}

// hasFuse reports whether a FUSE library was detected during Preflight.
func (m *Manager) hasFuse() bool {
	return m.hasFUSE
}

// resolveMethod picks the concrete mount method from a possibly-auto value.
func (m *Manager) resolveMethod(method Method) (Method, error) {
	switch method {
	case MethodFuse:
		if !m.hasFUSE {
			return "", fmt.Errorf(
				"mount_method \"fuse\" requested but FUSE not available; " +
					"install macFUSE/FUSE-T (macOS) or fuse3 (Linux), or use mount_method: \"nfs\"")
		}
		return MethodFuse, nil

	case MethodNFS:
		return MethodNFS, nil

	default: // auto
		if m.hasFUSE {
			return MethodFuse, nil
		}
		return MethodNFS, nil
	}
}

// Add registers a KB path. If remote is non-empty, starts an rclone mount at
// mountpoint using the given method. If remote is empty, validates mountpoint
// as a local directory and ignores method/extraArgs.
// The rclone process runs as a direct child (non-daemon) and is tracked for
// cleanup via Unmount/UnmountAll.
// After a successful mount, Add invokes the OnMounted hook from ctx (if any)
// and stores the returned stop func for cleanup.
func (m *Manager) Add(ctx context.Context, name, remote, mountpoint string, method Method, extraArgs map[string]string) (retErr error) {
	if mountpoint == "" {
		return fmt.Errorf("mount path is required for all kbs")
	}
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))

	// Record the outcome against this mountpoint so callers can query MountError.
	defer func() { m.recordMountErr(mountpoint, retErr) }()

	m.mu.Lock()
	if _, exists := m.entries[mountpoint]; exists {
		m.mu.Unlock()
		return fmt.Errorf("mountpoint %q already registered", mountpoint)
	}
	m.mu.Unlock()

	e := &entry{remote: remote, mountpoint: mountpoint}

	if remote == "" {
		fi, err := os.Stat(mountpoint)
		if err != nil {
			return fmt.Errorf("local directory %q: %w", mountpoint, err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("local path %q is not a directory", mountpoint)
		}
	} else {
		resolved, err := m.resolveMethod(method)
		if err != nil {
			return err
		}
		e.method = resolved

		if err := os.MkdirAll(mountpoint, 0o755); err != nil {
			return fmt.Errorf("create mountpoint %q: %w", mountpoint, err)
		}
		if m.checkMount(mountpoint) {
			if err := m.doUnmount(&entry{mountpoint: mountpoint, method: resolved}); err != nil {
				return fmt.Errorf("unmount stale mount at %q: %w", mountpoint, err)
			}
		} else {
			entries, err := os.ReadDir(mountpoint)
			if err != nil {
				return fmt.Errorf("read mountpoint %q: %w", mountpoint, err)
			}
			if len(entries) > 0 {
				return fmt.Errorf("mountpoint %q is not empty; remote mounts require an empty directory", mountpoint)
			}
		}
		cmd, err := m.rcloneMount(remote, mountpoint, resolved, extraArgs)
		if err != nil {
			return err
		}
		e.cmd = cmd
	}

	m.mu.Lock()
	m.entries[mountpoint] = e
	m.mu.Unlock()

	if hook, err := OnMountedFromContext(ctx); err == nil {
		if stop, hookErr := hook(name, mountpoint); hookErr != nil {
			slog.Error("on-mounted hook", "kb", name, "err", hookErr)
		} else if stop != nil {
			m.addStopFunc(mountpoint, stop)
		}
	}

	return nil
}

func (m *Manager) addStopFunc(mountpoint string, stop func()) {
	m.mu.Lock()
	m.stops[mountpoint] = stop
	m.mu.Unlock()
}

func (m *Manager) runAndClearStopFunc(mountpoint string) {
	m.mu.Lock()
	stop, ok := m.stops[mountpoint]
	delete(m.stops, mountpoint)
	m.mu.Unlock()
	if ok && stop != nil {
		stop()
	}
}

// rcloneMount starts rclone as a direct child process (no --daemon).
// Returns the running Cmd so the caller can track and kill it.
func (m *Manager) rcloneMount(remote, mountpoint string, method Method, extraArgs map[string]string) (*exec.Cmd, error) {
	subcmd := "mount"
	if method == MethodNFS {
		subcmd = "nfsmount"
	}

	merged := make(map[string]string, len(DefaultRcloneArgs)+len(extraArgs))
	for k, v := range DefaultRcloneArgs {
		merged[k] = v
	}
	for k, v := range extraArgs {
		merged[k] = v
	}

	args := []string{subcmd, remote, mountpoint}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := merged[k]
		if v == "" {
			args = append(args, "--"+k)
		} else {
			args = append(args, "--"+k, v)
		}
	}

	cmd := exec.Command("rclone", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rclone %s %q at %q: %w", subcmd, remote, mountpoint, err)
	}

	// Reap the child in the background so it doesn't become a zombie.
	go func() { _ = cmd.Wait() }()

	return cmd, nil
}

// Unmount stops the rclone mount at the given mountpoint.
// No-op for plain local directories.
func (m *Manager) Unmount(mountpoint string) error {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))

	m.mu.Lock()
	e, ok := m.entries[mountpoint]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("mountpoint %q not registered", mountpoint)
	}

	m.runAndClearStopFunc(mountpoint)

	if e.remote == "" {
		return nil
	}

	return m.doUnmount(e)
}

func (m *Manager) doUnmount(e *entry) error {
	mp := e.mountpoint
	if resolved, err := filepath.EvalSymlinks(mp); err == nil {
		mp = resolved
	}

	var umountErr error

	switch {
	case runtime.GOOS == "darwin":
		cmd := exec.Command("umount", mp)
		if out, err := cmd.CombinedOutput(); err != nil {
			umountErr = fmt.Errorf("unmount %q: %s: %w", mp, strings.TrimSpace(string(out)), err)
		}
	case e.method == MethodFuse && m.fuseUnmountBin != "":
		cmd := exec.Command(m.fuseUnmountBin, "-u", mp)
		if out, err := cmd.CombinedOutput(); err != nil {
			umountErr = fmt.Errorf("unmount %q: %s: %w", mp, strings.TrimSpace(string(out)), err)
		}
	default:
		cmd := exec.Command("umount", mp)
		if out, err := cmd.CombinedOutput(); err != nil {
			umountErr = fmt.Errorf("unmount %q: %s: %w", mp, strings.TrimSpace(string(out)), err)
		}
	}

	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Signal(syscall.SIGTERM)
	}

	return umountErr
}

// IsMounted returns true if the mountpoint has an active mount.
// Returns false for plain local directories.
func (m *Manager) IsMounted(mountpoint string) bool {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))

	m.mu.Lock()
	e, ok := m.entries[mountpoint]
	m.mu.Unlock()
	if !ok {
		return false
	}
	if e.remote == "" {
		return false
	}

	return m.checkMount(mountpoint)
}

func (m *Manager) checkMount(mountpoint string) bool {
	// Resolve symlinks so /tmp matches /private/tmp on macOS.
	if resolved, err := filepath.EvalSymlinks(mountpoint); err == nil {
		mountpoint = resolved
	}

	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/mounts")
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == mountpoint {
				return true
			}
		}
		return false

	case "darwin":
		out, err := exec.Command("mount").Output()
		if err != nil {
			return false
		}
		target := " on " + mountpoint + " "
		return strings.Contains(string(out), target)

	default:
		return false
	}
}

// unmountAll unmounts all registered remote mounts. Called by the cleanup func
// returned from ServeSetup.
func (m *Manager) unmountAll() error {
	m.mu.Lock()
	entries := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	m.mu.Unlock()

	var errs []error
	for _, e := range entries {
		m.runAndClearStopFunc(e.mountpoint)
		if e.remote == "" {
			continue
		}
		if err := m.doUnmount(e); err != nil {
			errs = append(errs, err)
		}
	}

	m.mu.Lock()
	m.entries = make(map[string]*entry)
	m.stops = make(map[string]func())
	m.mu.Unlock()

	if len(errs) > 0 {
		return fmt.Errorf("unmount errors: %v", errs)
	}
	return nil
}

// Deregister removes a mountpoint from tracking without unmounting.
// Use after Unmount to allow a subsequent Add for the same path.
func (m *Manager) Deregister(mountpoint string) {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))
	m.runAndClearStopFunc(mountpoint)
	m.mu.Lock()
	delete(m.entries, mountpoint)
	m.mu.Unlock()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
