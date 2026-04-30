package mount

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
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

// DarwinRcloneArgs suppress macOS metadata artifacts where the active rclone
// mount backend supports these flags. Cleanup still handles drivers that leak
// AppleDouble files despite the flags.
var DarwinRcloneArgs = map[string]string{
	"noappledouble": "",
	"noapplexattr":  "",
}

var disallowedRcloneArgs = map[string]string{
	"daemon": "AKB tracks rclone as a direct child process",
}

var writeBackGraceBuffer = time.Second

const (
	// SyncAssuranceLocalNoop means no remote sync was needed for a local KB.
	SyncAssuranceLocalNoop = "local_noop"
	// SyncAssuranceWriteBackElapsedMountHealthy means the write-back window
	// elapsed and the tracked mount process remained alive.
	SyncAssuranceWriteBackElapsedMountHealthy = "write_back_elapsed_mount_healthy"
)

// RcloneDurabilitySettings describes effective rclone settings that affect
// remote write visibility and cross-host backsync behavior.
type RcloneDurabilitySettings struct {
	VFSWriteBack string `json:"vfs_write_back"`
	DirCacheTime string `json:"dir_cache_time"`
	PollInterval string `json:"poll_interval"`
}

// MountDetails describes the concrete rclone mount mode and best-effort OS
// verification details for a registered remote KB.
type MountDetails struct {
	RcloneSubcommand string `json:"rclone_subcommand"`
	FuseProvider     string `json:"fuse_provider,omitempty"`
	FuseDetectedFrom string `json:"fuse_detected_from,omitempty"`
	FuseUnmountBin   string `json:"fuse_unmount_binary,omitempty"`
	OSMountType      string `json:"os_mount_type,omitempty"`
	OSMountSource    string `json:"os_mount_source,omitempty"`
}

// SyncResult describes the assurance AKB can provide after a mount sync wait.
type SyncResult struct {
	Assurance string
	Waited    time.Duration
}

// Method determines how remote storage is mounted locally.
type Method string

const (
	MethodAuto Method = ""     // prefer FUSE, fall back to NFS
	MethodFuse Method = "fuse" // rclone mount (requires FUSE library)
	MethodNFS  Method = "nfs"  // rclone nfsmount (no FUSE dependency)
)

type entry struct {
	remote       string // empty for plain local directories
	mountpoint   string
	method       Method    // resolved method (never MethodAuto)
	cmd          *exec.Cmd // rclone child process (nil for local dirs)
	rcloneArgs   map[string]string
	exitExpected bool
	exited       bool
	exitErr      error
	waitDone     chan struct{}
}

// Manager tracks rclone mounts and plain local directories.
type Manager struct {
	mu               sync.Mutex
	entries          map[string]*entry // mountpoint -> entry
	stops            map[string]func() // mountpoint -> watcher stop func
	mountErrs        map[string]error  // mountpoint -> last Add error (nil = no error)
	fuseUnmountBin   string            // "fusermount3"/"fusermount" (Linux only, for FUSE unmounts)
	fuseProvider     string            // "macfuse"/"osxfuse"/"fuse-t" when known
	fuseDetectedFrom string            // path or probe that established FUSE availability
	hasFUSE          bool
	preflightCalled  bool
}

type fuseProbeResult struct {
	available    bool
	provider     string
	detectedFrom string
	unmountBin   string
}

type osMountInfo struct {
	source string
	fsType string
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
		probe := probeDarwinFuse(pathExists)
		m.hasFUSE = probe.available
		m.fuseProvider = probe.provider
		m.fuseDetectedFrom = probe.detectedFrom

	case "linux":
		probe := probeLinuxFuse(pathExists, exec.LookPath)
		m.hasFUSE = probe.available
		m.fuseProvider = probe.provider
		m.fuseDetectedFrom = probe.detectedFrom
		m.fuseUnmountBin = probe.unmountBin

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

func probeDarwinFuse(pathExistsFn func(string) bool) fuseProbeResult {
	// cgofuse probes these dylibs in order: macFUSE, osxfuse, FUSE-T.
	// Also check the .fs bundles used by older installers.
	probes := []struct {
		path     string
		provider string
	}{
		{path: "/usr/local/lib/libfuse.2.dylib", provider: "macfuse"},
		{path: "/usr/local/lib/libosxfuse.2.dylib", provider: "osxfuse"},
		{path: "/usr/local/lib/libfuse-t.dylib", provider: "fuse-t"},
		{path: "/Library/Filesystems/macfuse.fs", provider: "macfuse"},
		{path: "/Library/Filesystems/fuse-t.fs", provider: "fuse-t"},
	}
	for _, probe := range probes {
		if pathExistsFn(probe.path) {
			return fuseProbeResult{
				available:    true,
				provider:     probe.provider,
				detectedFrom: probe.path,
			}
		}
	}
	return fuseProbeResult{}
}

func probeLinuxFuse(pathExistsFn func(string) bool, lookPathFn func(string) (string, error)) fuseProbeResult {
	if !pathExistsFn("/dev/fuse") {
		return fuseProbeResult{}
	}
	for _, name := range []string{"fusermount3", "fusermount"} {
		bin, err := lookPathFn(name)
		if err == nil {
			return fuseProbeResult{
				available:    true,
				detectedFrom: "/dev/fuse + " + bin,
				unmountBin:   bin,
			}
		}
	}
	return fuseProbeResult{}
}

// EffectiveRcloneArgs returns the validated rclone args after applying
// environment expansion and per-KB overrides on top of DefaultRcloneArgs.
func EffectiveRcloneArgs(extraArgs map[string]string) (map[string]string, error) {
	merged := defaultRcloneArgs()
	for k, v := range extraArgs {
		key := os.ExpandEnv(k)
		merged[key] = os.ExpandEnv(v)
	}
	for k, reason := range disallowedRcloneArgs {
		if _, ok := merged[k]; ok {
			return nil, fmt.Errorf("rclone arg %q is not supported: %s", k, reason)
		}
	}
	if _, err := RcloneWriteBackDurationFromArgs(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

func defaultRcloneArgs() map[string]string {
	merged := make(map[string]string, len(DefaultRcloneArgs)+len(DarwinRcloneArgs))
	for k, v := range DefaultRcloneArgs {
		merged[k] = v
	}
	if runtime.GOOS == "darwin" {
		for k, v := range DarwinRcloneArgs {
			merged[k] = v
		}
	}
	return merged
}

// RcloneWriteBackDuration returns the effective vfs-write-back duration for a
// KB's rclone_args after applying defaults and validation.
func RcloneWriteBackDuration(extraArgs map[string]string) (time.Duration, error) {
	args, err := EffectiveRcloneArgs(extraArgs)
	if err != nil {
		return 0, err
	}
	return RcloneWriteBackDurationFromArgs(args)
}

// RcloneWriteBackDurationFromArgs parses vfs-write-back from already-merged
// rclone args.
func RcloneWriteBackDurationFromArgs(args map[string]string) (time.Duration, error) {
	raw, ok := args["vfs-write-back"]
	if !ok {
		return 0, fmt.Errorf("rclone arg %q is required", "vfs-write-back")
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse rclone arg %q=%q: %w", "vfs-write-back", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("rclone arg %q must be non-negative", "vfs-write-back")
	}
	return d, nil
}

// RcloneDurability returns effective rclone settings that callers can surface
// to agents without exposing every mount flag.
func RcloneDurability(extraArgs map[string]string) (RcloneDurabilitySettings, error) {
	args, err := EffectiveRcloneArgs(extraArgs)
	if err != nil {
		return RcloneDurabilitySettings{}, err
	}
	return RcloneDurabilitySettings{
		VFSWriteBack: args["vfs-write-back"],
		DirCacheTime: args["dir-cache-time"],
		PollInterval: args["poll-interval"],
	}, nil
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

// ResolvedMethod returns the concrete mount method chosen for a registered
// remote KB. It reports false for local directories and unknown mountpoints.
func (m *Manager) ResolvedMethod(mountpoint string) (Method, bool) {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))

	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entries[mountpoint]
	if !ok || e.remote == "" || e.method == MethodAuto || e.exited {
		return "", false
	}
	return e.method, true
}

// MountDetails returns concrete rclone mount details for a registered remote KB.
// OS mount-table fields are best effort and omitted when unavailable.
func (m *Manager) MountDetails(mountpoint string) (MountDetails, bool) {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))

	m.mu.Lock()
	e, ok := m.entries[mountpoint]
	if !ok || e.remote == "" || e.method == MethodAuto || e.exited {
		m.mu.Unlock()
		return MountDetails{}, false
	}
	method := e.method
	fuseProvider := m.fuseProvider
	fuseDetectedFrom := m.fuseDetectedFrom
	fuseUnmountBin := m.fuseUnmountBin
	m.mu.Unlock()

	details := MountDetails{
		RcloneSubcommand: rcloneSubcommand(method),
	}
	if method == MethodFuse {
		details.FuseProvider = fuseProvider
		details.FuseDetectedFrom = fuseDetectedFrom
		details.FuseUnmountBin = fuseUnmountBin
	}
	if info, ok := lookupOSMountInfo(mountpoint); ok {
		details.OSMountType = info.fsType
		details.OSMountSource = info.source
	}
	return details, true
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
	remote = os.ExpandEnv(remote)
	method = Method(os.ExpandEnv(string(method)))

	// Record the outcome against this mountpoint so callers can query MountError.
	defer func() { m.recordMountErr(mountpoint, retErr) }()

	m.mu.Lock()
	if _, exists := m.entries[mountpoint]; exists {
		m.mu.Unlock()
		return fmt.Errorf("mountpoint %q already registered", mountpoint)
	}
	m.mu.Unlock()

	e := &entry{remote: remote, mountpoint: mountpoint, rcloneArgs: extraArgs}

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
		if err := m.probeRemote(ctx, remote); err != nil {
			return err
		}
		cmd, err := m.rcloneMount(remote, mountpoint, resolved, extraArgs)
		if err != nil {
			return err
		}
		e.cmd = cmd
		e.waitDone = make(chan struct{})
		m.watchRclone(e)

		waitCtx, waitCancel := context.WithTimeout(ctx, mountCheckTimeout())
		defer waitCancel()
		if err := m.waitForMount(waitCtx, mountpoint); err != nil {
			_ = m.doUnmount(e)
			return fmt.Errorf("mount did not become ready: %w", err)
		}
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

// probeRemote runs "rclone lsd <remote> --max-depth 0" with a 15-second
// timeout to verify the remote is reachable before committing to a full mount.
// Returns a descriptive error (including rclone stderr) on failure.
func (m *Manager) probeRemote(ctx context.Context, remote string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rclone", "lsd", remote, "--max-depth", "0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remote probe %q: %s: %w",
			remote, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// rcloneMount starts rclone as a direct child process (no --daemon).
// Returns the running Cmd so the caller can track and kill it.
func (m *Manager) rcloneMount(remote, mountpoint string, method Method, extraArgs map[string]string) (*exec.Cmd, error) {
	subcmd := rcloneSubcommand(method)

	merged, err := EffectiveRcloneArgs(extraArgs)
	if err != nil {
		return nil, err
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

	return cmd, nil
}

func rcloneSubcommand(method Method) string {
	if method == MethodNFS {
		return "nfsmount"
	}
	return "mount"
}

func (m *Manager) watchRclone(e *entry) {
	go func() {
		err := e.cmd.Wait()
		m.recordRcloneExit(e, err)
	}()
}

func (m *Manager) recordRcloneExit(e *entry, waitErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e.exited = true
	e.exitErr = waitErr
	if e.waitDone != nil {
		close(e.waitDone)
	}
	if e.exitExpected {
		return
	}

	err := fmt.Errorf("rclone process exited unexpectedly")
	if waitErr != nil {
		err = fmt.Errorf("rclone process exited unexpectedly: %w", waitErr)
	}
	m.mountErrs[e.mountpoint] = err
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

	var waitErr error
	if e.remote != "" && e.cmd != nil && m.checkMount(mp) {
		if err := CleanMetadataArtifacts(mp); err != nil {
			waitErr = err
		} else {
			_, waitErr = m.waitForWriteBack(e)
		}
	}
	m.markExitExpected(e)

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

	if waitErr != nil && umountErr != nil {
		return fmt.Errorf("%v; %w", waitErr, umountErr)
	}
	if waitErr != nil {
		return waitErr
	}
	return umountErr
}

// Sync waits for the configured rclone write-back window on a mounted remote KB.
// It does not prove remote object durability; it only verifies that the
// write-back window elapsed while the tracked mount process stayed alive.
func (m *Manager) Sync(mountpoint string) (SyncResult, error) {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))

	m.mu.Lock()
	e, ok := m.entries[mountpoint]
	remote := ""
	exited := false
	exitErr := error(nil)
	if ok {
		remote = e.remote
		exited = e.exited
		exitErr = e.exitErr
	}
	m.mu.Unlock()
	if !ok {
		return SyncResult{}, fmt.Errorf("mountpoint %q not registered", mountpoint)
	}
	if remote == "" {
		return SyncResult{Assurance: SyncAssuranceLocalNoop}, nil
	}
	if exited {
		return SyncResult{}, rcloneExitedDuringWriteBackError(exitErr)
	}
	if !m.checkMount(mountpoint) {
		return SyncResult{}, fmt.Errorf("mountpoint %q is not mounted", mountpoint)
	}

	if err := CleanMetadataArtifacts(mountpoint); err != nil {
		return SyncResult{}, err
	}
	waited, err := m.waitForWriteBack(e)
	if err != nil {
		return SyncResult{}, err
	}
	if !m.IsMounted(mountpoint) {
		return SyncResult{}, fmt.Errorf("mountpoint %q is not mounted after sync wait", mountpoint)
	}
	return SyncResult{
		Assurance: SyncAssuranceWriteBackElapsedMountHealthy,
		Waited:    waited,
	}, nil
}

func (m *Manager) waitForWriteBack(e *entry) (time.Duration, error) {
	if e == nil || e.remote == "" || e.cmd == nil {
		return 0, nil
	}
	writeBack, err := RcloneWriteBackDuration(e.rcloneArgs)
	if err != nil {
		return 0, err
	}
	wait := writeBack + writeBackGraceBuffer
	if wait <= 0 {
		return 0, nil
	}

	m.mu.Lock()
	exited := e.exited
	exitErr := e.exitErr
	waitDone := e.waitDone
	m.mu.Unlock()
	if exited {
		return 0, rcloneExitedDuringWriteBackError(exitErr)
	}
	if waitDone == nil {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		<-timer.C
		return wait, nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		m.mu.Lock()
		exited = e.exited
		exitErr = e.exitErr
		m.mu.Unlock()
		if exited {
			return 0, rcloneExitedDuringWriteBackError(exitErr)
		}
		return wait, nil
	case <-waitDone:
		m.mu.Lock()
		exitErr = e.exitErr
		m.mu.Unlock()
		return 0, rcloneExitedDuringWriteBackError(exitErr)
	}
}

func rcloneExitedDuringWriteBackError(exitErr error) error {
	if exitErr != nil {
		return fmt.Errorf("rclone exited before write-back grace completed: %w", exitErr)
	}
	return fmt.Errorf("rclone exited before write-back grace completed")
}

// CleanMetadataArtifacts removes disposable macOS metadata files that can be
// created by writing through FUSE mounts.
func CleanMetadataArtifacts(root string) error {
	root = filepath.Clean(os.ExpandEnv(root))
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || !isMetadataArtifact(d.Name()) {
			return nil
		}
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

func isMetadataArtifact(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

func (m *Manager) markExitExpected(e *entry) {
	if e == nil {
		return
	}
	m.mu.Lock()
	e.exitExpected = true
	m.mu.Unlock()
}

// IsMounted returns true if the mountpoint has an active mount.
// Returns false for plain local directories.
func (m *Manager) IsMounted(mountpoint string) bool {
	mountpoint = filepath.Clean(os.ExpandEnv(mountpoint))

	m.mu.Lock()
	e, ok := m.entries[mountpoint]
	remote := ""
	exited := false
	if ok {
		remote = e.remote
		exited = e.exited
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	if remote == "" {
		return false
	}
	if exited {
		return false
	}

	return m.checkMount(mountpoint)
}

func (m *Manager) checkMount(mountpoint string) bool {
	_, ok := lookupOSMountInfo(mountpoint)
	return ok
}

func lookupOSMountInfo(mountpoint string) (osMountInfo, bool) {
	// Resolve symlinks so /tmp matches /private/tmp on macOS.
	if resolved, err := filepath.EvalSymlinks(mountpoint); err == nil {
		mountpoint = resolved
	}
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/mounts")
		if err != nil {
			return osMountInfo{}, false
		}
		return parseLinuxMounts(string(data), mountpoint)

	case "darwin":
		out, err := exec.Command("mount").Output()
		if err != nil {
			return osMountInfo{}, false
		}
		return parseDarwinMountOutput(string(out), mountpoint)

	default:
		return osMountInfo{}, false
	}
}

func parseLinuxMounts(data, mountpoint string) (osMountInfo, bool) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		target := unescapeProcMountField(fields[1])
		if target == mountpoint {
			return osMountInfo{
				source: unescapeProcMountField(fields[0]),
				fsType: fields[2],
			}, true
		}
	}
	return osMountInfo{}, false
}

func parseDarwinMountOutput(data, mountpoint string) (osMountInfo, bool) {
	for _, line := range strings.Split(data, "\n") {
		onIdx := strings.Index(line, " on ")
		if onIdx < 0 {
			continue
		}
		rest := line[onIdx+len(" on "):]
		detailIdx := strings.LastIndex(rest, " (")
		if detailIdx < 0 {
			continue
		}
		target := rest[:detailIdx]
		if target != mountpoint {
			continue
		}
		details := strings.TrimSuffix(rest[detailIdx+len(" ("):], ")")
		fsType, _, _ := strings.Cut(details, ",")
		return osMountInfo{
			source: line[:onIdx],
			fsType: strings.TrimSpace(fsType),
		}, true
	}
	return osMountInfo{}, false
}

func unescapeProcMountField(field string) string {
	replacer := strings.NewReplacer(
		`\\`, `\`,
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(field)
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

// mountCheckPollInterval returns the interval between mount-readiness polls.
// Reads AKB_MOUNT_CHECK_POLL_MS from the environment (default 200 ms).
// Invalid or missing values fall back to the default silently.
func mountCheckPollInterval() time.Duration {
	const defaultMs = 200
	if s := os.Getenv("AKB_MOUNT_CHECK_POLL_MS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultMs * time.Millisecond
}

// mountCheckTimeout returns the maximum time to wait for a remote mount to
// become ready. Reads AKB_MOUNT_CHECK_TIMEOUT_MS from the environment
// (default 30000 ms). Invalid or missing values fall back to the default silently.
func mountCheckTimeout() time.Duration {
	const defaultMs = 30_000
	if s := os.Getenv("AKB_MOUNT_CHECK_TIMEOUT_MS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultMs * time.Millisecond
}

// waitForMount polls checkMount until the OS mount table shows mountpoint as
// mounted or ctx is cancelled. It checks once immediately before starting the
// ticker so already-mounted paths return without delay.
func (m *Manager) waitForMount(ctx context.Context, mountpoint string) error {
	if m.checkMount(mountpoint) {
		return nil
	}
	ticker := time.NewTicker(mountCheckPollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for mount at %q: %w", mountpoint, ctx.Err())
		case <-ticker.C:
			if m.checkMount(mountpoint) {
				return nil
			}
		}
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
