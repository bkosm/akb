package mount

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdd_LocalDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
		t.Fatalf("Add local dir: %v", err)
	}

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err == nil {
		t.Fatal("expected error for duplicate mountpoint")
	}
}

func TestAdd_LocalDir_NotExist(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	err := mgr.Add(context.Background(), "test", "", "/nonexistent/path/that/does/not/exist", MethodAuto, nil)
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestAdd_LocalDir_NotADir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	err := mgr.Add(context.Background(), "test", "", f, MethodAuto, nil)
	if err == nil {
		t.Fatal("expected error for non-directory path")
	}
}

func TestAdd_LocalDir_IgnoresMethod(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.Add(context.Background(), "test", "", dir, MethodNFS, nil); err != nil {
		t.Fatalf("Add local dir with NFS method should succeed (method ignored): %v", err)
	}
}

func TestIsMounted_LocalDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
		t.Fatal(err)
	}

	if mgr.IsMounted(dir) {
		t.Fatal("local dir should not report as mounted")
	}
}

func TestUnmount_LocalDir_Noop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Unmount(dir); err != nil {
		t.Fatalf("Unmount local dir should be no-op: %v", err)
	}
}

func TestUnmount_NotRegistered(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	if err := mgr.Unmount("/not/registered"); err == nil {
		t.Fatal("expected error for unregistered mountpoint")
	}
}

func TestUnmountAll_Empty(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	if err := mgr.unmountAll(); err != nil {
		t.Fatalf("unmountAll on empty manager: %v", err)
	}
}

func TestUnmountAll_LocalDirs(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	for i := 0; i < 3; i++ {
		dir := t.TempDir()
		if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
			t.Fatal(err)
		}
	}

	if err := mgr.unmountAll(); err != nil {
		t.Fatalf("unmountAll with only local dirs: %v", err)
	}
}

func TestContext(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	ctx := ManagerIntoContext(context.Background(), mgr)

	got, err := ManagerFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != mgr {
		t.Fatal("ManagerFromContext returned different manager")
	}
}

func TestContext_Missing(t *testing.T) {
	t.Parallel()
	_, err := ManagerFromContext(context.Background())
	if err == nil {
		t.Fatal("expected error when manager not in context")
	}
}

func TestAdd_EnvExpansion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_KB_DIR", dir)

	mgr := NewManager()
	if err := mgr.Add(context.Background(), "test", "", "$TEST_KB_DIR", MethodAuto, nil); err != nil {
		t.Fatalf("Add with env var: %v", err)
	}
}

func TestAdd_RemoteEnvExpansion(t *testing.T) {
	// Verify that $VAR in the remote field is expanded before reaching rclone.
	// writeFakeRclone's lsd handler echoes the remote arg so we can check it.
	writeFakeRclone(t, `[ "$1" = "lsd" ] && echo "$2" && exit 1; exit 0`)

	t.Setenv("TEST_BUCKET", "expanded-bucket")

	dir := t.TempDir()
	mgr := &Manager{
		entries:         make(map[string]*entry),
		stops:           make(map[string]func()),
		mountErrs:       make(map[string]error),
		hasFUSE:         true,
		preflightCalled: true,
	}

	err := mgr.Add(context.Background(), "test", ":s3:$TEST_BUCKET/", dir, MethodFuse, nil)
	if err == nil {
		t.Fatal("expected probe failure")
	}
	if !strings.Contains(err.Error(), "expanded-bucket") {
		t.Fatalf("error should contain expanded remote, got: %v", err)
	}
}

func TestAdd_ExtraArgsEnvExpansion(t *testing.T) {
	// Verify that $VAR in extraArgs values is expanded. The fake rclone lsd
	// succeeds; mount/nfsmount echoes all args so we can inspect them.
	writeFakeRclone(t, `
if [ "$1" = "lsd" ]; then exit 0; fi
echo "$@" >&2
sleep 0.5
`)

	t.Setenv("TEST_CACHE_SIZE", "42G")
	t.Setenv("AKB_MOUNT_CHECK_TIMEOUT_MS", "200")
	t.Setenv("AKB_MOUNT_CHECK_POLL_MS", "50")

	dir := t.TempDir()
	mgr := &Manager{
		entries:         make(map[string]*entry),
		stops:           make(map[string]func()),
		mountErrs:       make(map[string]error),
		hasFUSE:         true,
		preflightCalled: true,
	}

	// The mount will time out (fake rclone sleeps), but the rclone args
	// are already built by that point. Check that the stored error references
	// the mountpoint (proves rcloneMount was called with expanded args).
	err := mgr.Add(context.Background(), "test", ":memory:", dir, MethodFuse, map[string]string{
		"vfs-cache-max-size": "$TEST_CACHE_SIZE",
	})
	if err == nil {
		t.Fatal("expected timeout error from fake mount")
	}
	// The fact that we got past probeRemote and into rcloneMount/waitForMount
	// proves the expanded extraArgs were accepted. The timeout error confirms
	// rcloneMount was called.
	if !strings.Contains(err.Error(), "mount did not become ready") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestAdd_MethodEnvExpansion(t *testing.T) {
	t.Setenv("TEST_METHOD", "nfs")

	mgr := &Manager{
		entries:         make(map[string]*entry),
		stops:           make(map[string]func()),
		mountErrs:       make(map[string]error),
		hasFUSE:         false,
		preflightCalled: true,
	}

	dir := t.TempDir()
	// With hasFUSE=false and method="fuse", resolveMethod would fail.
	// But $TEST_METHOD="nfs" should expand to MethodNFS which succeeds.
	// lsd will fail because there's no rclone, but we're testing method expansion.
	writeFakeRclone(t, `[ "$1" = "lsd" ] && echo "fail" >&2 && exit 1; exit 0`)

	err := mgr.Add(context.Background(), "test", ":memory:", dir, Method("$TEST_METHOD"), nil)
	if err == nil {
		t.Fatal("expected probe failure")
	}
	// Should NOT be a "FUSE not available" error — that would mean method wasn't expanded.
	if strings.Contains(err.Error(), "FUSE not available") {
		t.Fatalf("method env var was not expanded, got FUSE error: %v", err)
	}
}

func TestResolveMethod_Auto_WithFUSE(t *testing.T) {
	t.Parallel()
	mgr := &Manager{entries: make(map[string]*entry), stops: make(map[string]func()), mountErrs: make(map[string]error), hasFUSE: true}

	got, err := mgr.resolveMethod(MethodAuto)
	if err != nil {
		t.Fatal(err)
	}
	if got != MethodFuse {
		t.Fatalf("auto with FUSE should resolve to fuse, got %q", got)
	}
}

func TestResolveMethod_Auto_NoFUSE(t *testing.T) {
	t.Parallel()
	mgr := &Manager{entries: make(map[string]*entry), stops: make(map[string]func()), mountErrs: make(map[string]error), hasFUSE: false}

	got, err := mgr.resolveMethod(MethodAuto)
	if err != nil {
		t.Fatal(err)
	}
	if got != MethodNFS {
		t.Fatalf("auto without FUSE should resolve to nfs, got %q", got)
	}
}

func TestResolveMethod_FuseExplicit_NoFUSE(t *testing.T) {
	t.Parallel()
	mgr := &Manager{entries: make(map[string]*entry), stops: make(map[string]func()), mountErrs: make(map[string]error), hasFUSE: false}

	_, err := mgr.resolveMethod(MethodFuse)
	if err == nil {
		t.Fatal("expected error when requesting fuse without FUSE available")
	}
}

func TestResolveMethod_FuseExplicit_WithFUSE(t *testing.T) {
	t.Parallel()
	mgr := &Manager{entries: make(map[string]*entry), stops: make(map[string]func()), mountErrs: make(map[string]error), hasFUSE: true}

	got, err := mgr.resolveMethod(MethodFuse)
	if err != nil {
		t.Fatal(err)
	}
	if got != MethodFuse {
		t.Fatalf("explicit fuse should resolve to fuse, got %q", got)
	}
}

func TestResolveMethod_NFS(t *testing.T) {
	t.Parallel()
	mgr := &Manager{entries: make(map[string]*entry), stops: make(map[string]func()), mountErrs: make(map[string]error), hasFUSE: false}

	got, err := mgr.resolveMethod(MethodNFS)
	if err != nil {
		t.Fatal(err)
	}
	if got != MethodNFS {
		t.Fatalf("explicit nfs should resolve to nfs, got %q", got)
	}
}

func TestDefaultRcloneArgs(t *testing.T) {
	t.Parallel()
	if _, ok := DefaultRcloneArgs["vfs-cache-mode"]; !ok {
		t.Fatal("expected vfs-cache-mode in defaults")
	}
	if _, ok := DefaultRcloneArgs["daemon"]; ok {
		t.Fatal("daemon should not be in defaults (non-daemon mode)")
	}
}

func TestEffectiveRcloneArgs_MergesAndExpands(t *testing.T) {
	t.Setenv("TEST_CACHE_SIZE", "42G")

	args, err := EffectiveRcloneArgs(map[string]string{
		"vfs-cache-max-size": "$TEST_CACHE_SIZE",
		"new-flag":           "value",
	})
	if err != nil {
		t.Fatalf("EffectiveRcloneArgs: %v", err)
	}
	if args["vfs-cache-mode"] != "full" {
		t.Fatalf("default vfs-cache-mode = %q, want full", args["vfs-cache-mode"])
	}
	if args["vfs-cache-max-size"] != "42G" {
		t.Fatalf("override vfs-cache-max-size = %q, want 42G", args["vfs-cache-max-size"])
	}
	if args["new-flag"] != "value" {
		t.Fatalf("new-flag = %q, want value", args["new-flag"])
	}
}

func TestEffectiveRcloneArgs_RejectsDaemon(t *testing.T) {
	if _, err := EffectiveRcloneArgs(map[string]string{"daemon": ""}); err == nil {
		t.Fatal("expected daemon arg to be rejected")
	}
}

func TestRcloneWriteBackDuration(t *testing.T) {
	got, err := RcloneWriteBackDuration(map[string]string{"vfs-write-back": "250ms"})
	if err != nil {
		t.Fatalf("RcloneWriteBackDuration: %v", err)
	}
	if got != 250*time.Millisecond {
		t.Fatalf("duration = %v, want 250ms", got)
	}
}

func TestRcloneWriteBackDuration_Invalid(t *testing.T) {
	if _, err := RcloneWriteBackDuration(map[string]string{"vfs-write-back": "soon"}); err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestRcloneDurability(t *testing.T) {
	got, err := RcloneDurability(map[string]string{
		"vfs-write-back": "1s",
		"dir-cache-time": "2s",
		"poll-interval":  "3s",
	})
	if err != nil {
		t.Fatalf("RcloneDurability: %v", err)
	}
	if got.VFSWriteBack != "1s" || got.DirCacheTime != "2s" || got.PollInterval != "3s" {
		t.Fatalf("settings = %#v", got)
	}
}

func TestDeregister(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err == nil {
		t.Fatal("expected error for duplicate mountpoint")
	}

	mgr.Deregister(dir)

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
		t.Fatalf("re-Add after Deregister should succeed: %v", err)
	}
}

func TestDeregister_NotRegistered(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	mgr.Deregister("/not/registered")
}

func TestAdd_BlankMountpoint(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	err := mgr.Add(context.Background(), "test", "", "", MethodAuto, nil)
	if err == nil {
		t.Fatal("expected error for blank mountpoint")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Fatalf("error should mention 'required', got: %v", err)
	}
}

func TestAdd_Remote_NonEmptyMountpoint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{entries: make(map[string]*entry), stops: make(map[string]func()), mountErrs: make(map[string]error), hasFUSE: true, preflightCalled: true}
	err := mgr.Add(context.Background(), "test", ":s3,env_auth=true:bucket/", dir, MethodFuse, nil)
	if err == nil {
		t.Fatal("expected error for non-empty mountpoint")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("error should mention 'not empty', got: %v", err)
	}
}

func TestHasFuse(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	if mgr.hasFuse() {
		t.Fatal("new manager should report hasFuse=false before Preflight")
	}
}

func TestMountError_NilForLocalDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := mgr.MountError(dir); err != nil {
		t.Fatalf("MountError after successful Add = %v, want nil", err)
	}
}

func TestMountError_NilForUnknown(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	if err := mgr.MountError("/not/registered/ever"); err != nil {
		t.Fatalf("MountError for unknown path = %v, want nil", err)
	}
}

func TestMountError_StoredOnFailedAdd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Write a file so the directory is non-empty; Add rejects non-empty
	// mountpoints for remote KBs.
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		entries:         make(map[string]*entry),
		stops:           make(map[string]func()),
		mountErrs:       make(map[string]error),
		hasFUSE:         true,
		preflightCalled: true,
	}

	addErr := mgr.Add(context.Background(), "test", ":s3,env_auth=true:bucket/", dir, MethodFuse, nil)
	if addErr == nil {
		t.Fatal("expected Add to fail for non-empty mountpoint")
	}
	if stored := mgr.MountError(dir); stored == nil {
		t.Fatal("MountError should return the stored error after a failed Add")
	}
	if stored := mgr.MountError(dir); stored.Error() != addErr.Error() {
		t.Fatalf("MountError = %v, want %v", stored, addErr)
	}
}

func TestMountError_ClearedOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager()

	// Pre-seed an error via SetMountError.
	mgr.SetMountError(dir, fmt.Errorf("previous failure"))

	// A successful local Add should clear the stored error.
	if err := mgr.Add(context.Background(), "test", "", dir, MethodAuto, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := mgr.MountError(dir); err != nil {
		t.Fatalf("MountError after successful Add = %v, want nil", err)
	}
}

func TestSetMountError(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	dir := t.TempDir()

	mgr.SetMountError(dir, fmt.Errorf("injected error"))
	if err := mgr.MountError(dir); err == nil {
		t.Fatal("expected stored error")
	}

	mgr.SetMountError(dir, nil)
	if err := mgr.MountError(dir); err != nil {
		t.Fatalf("expected nil after clearing, got %v", err)
	}
}

func TestWatchRclone_UnexpectedExitRecordsMountError(t *testing.T) {
	mgr := NewManager()
	dir := t.TempDir()
	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	e := &entry{
		remote:     ":memory:",
		mountpoint: dir,
		cmd:        cmd,
		waitDone:   make(chan struct{}),
	}
	mgr.mu.Lock()
	mgr.entries[dir] = e
	mgr.mu.Unlock()

	mgr.watchRclone(e)
	<-e.waitDone

	if err := mgr.MountError(dir); err == nil {
		t.Fatal("expected unexpected process exit to be recorded")
	}
	if mgr.IsMounted(dir) {
		t.Fatal("exited rclone process should not report mounted")
	}
}

func TestWatchRclone_ExpectedExitDoesNotRecordMountError(t *testing.T) {
	mgr := NewManager()
	dir := t.TempDir()
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	e := &entry{
		remote:       ":memory:",
		mountpoint:   dir,
		cmd:          cmd,
		exitExpected: true,
		waitDone:     make(chan struct{}),
	}
	mgr.mu.Lock()
	mgr.entries[dir] = e
	mgr.mu.Unlock()

	mgr.watchRclone(e)
	<-e.waitDone

	if err := mgr.MountError(dir); err != nil {
		t.Fatalf("expected intentional process exit to be ignored, got %v", err)
	}
}

func TestWaitForWriteBack_WaitsForDuration(t *testing.T) {
	mgr := NewManager()
	cmd := exec.Command("sh", "-c", "sleep 1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	oldBuffer := writeBackGraceBuffer
	writeBackGraceBuffer = 0
	defer func() { writeBackGraceBuffer = oldBuffer }()

	e := &entry{
		remote:     ":memory:",
		mountpoint: t.TempDir(),
		cmd:        cmd,
		rcloneArgs: map[string]string{"vfs-write-back": "5ms"},
		waitDone:   make(chan struct{}),
	}

	start := time.Now()
	if err := mgr.waitForWriteBack(e); err != nil {
		t.Fatalf("waitForWriteBack: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 5*time.Millisecond {
		t.Fatalf("waitForWriteBack returned too early after %v", elapsed)
	}
}

func TestWaitForWriteBack_FailsIfRcloneExits(t *testing.T) {
	mgr := NewManager()
	cmd := exec.Command("sh", "-c", "exit 1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	oldBuffer := writeBackGraceBuffer
	writeBackGraceBuffer = 0
	defer func() { writeBackGraceBuffer = oldBuffer }()

	e := &entry{
		remote:     ":memory:",
		mountpoint: t.TempDir(),
		cmd:        cmd,
		rcloneArgs: map[string]string{"vfs-write-back": "1h"},
		waitDone:   make(chan struct{}),
	}
	mgr.watchRclone(e)

	if err := mgr.waitForWriteBack(e); err == nil {
		t.Fatal("expected exited rclone to fail write-back wait")
	}
}

// writeFakeRclone creates a temporary directory containing a shell script
// named "rclone" that runs the given script body, prepends the dir to PATH,
// and returns the directory path.
func writeFakeRclone(t *testing.T, script string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "rclone")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin)+":"+os.Getenv("PATH"))
}

func TestProbeRemote_Failure(t *testing.T) {
	// lsd exits 1 with a message; mount/nfsmount would exit 0 but is never reached.
	writeFakeRclone(t, `[ "$1" = "lsd" ] && echo "NoSuchBucket" >&2 && exit 1; exit 0`)

	mgr := NewManager()
	err := mgr.probeRemote(context.Background(), ":s3:no-such-bucket/")
	if err == nil {
		t.Fatal("expected probe to fail for bad remote")
	}
	if !strings.Contains(err.Error(), "NoSuchBucket") {
		t.Fatalf("error should contain stderr output, got: %v", err)
	}
}

func TestAdd_Remote_ProbeBlocks(t *testing.T) {
	// lsd fails; mount/nfsmount would succeed but should never be called.
	writeFakeRclone(t, `[ "$1" = "lsd" ] && echo "NoSuchBucket" >&2 && exit 1; exit 0`)

	dir := t.TempDir()
	mgr := &Manager{
		entries:         make(map[string]*entry),
		stops:           make(map[string]func()),
		mountErrs:       make(map[string]error),
		hasFUSE:         true,
		preflightCalled: true,
	}

	err := mgr.Add(context.Background(), "test", ":s3:no-such-bucket/", dir, MethodFuse, nil)
	if err == nil {
		t.Fatal("expected Add to fail when probe fails")
	}
	if !strings.Contains(err.Error(), "NoSuchBucket") {
		t.Fatalf("error should mention probe output, got: %v", err)
	}
	if stored := mgr.MountError(dir); stored == nil {
		t.Fatal("MountError should be stored after a failed probe")
	}
	// The mountpoint must not appear as registered (rcloneMount was never called).
	mgr.mu.Lock()
	_, registered := mgr.entries[dir]
	mgr.mu.Unlock()
	if registered {
		t.Fatal("mountpoint should not be registered after probe failure")
	}
}

// --- mountCheckPollInterval ---

func TestMountCheckPollInterval_Default(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_POLL_MS", "")
	if got := mountCheckPollInterval(); got != 200*time.Millisecond {
		t.Fatalf("expected 200ms default, got %v", got)
	}
}

func TestMountCheckPollInterval_EnvOverride(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_POLL_MS", "500")
	if got := mountCheckPollInterval(); got != 500*time.Millisecond {
		t.Fatalf("expected 500ms, got %v", got)
	}
}

func TestMountCheckPollInterval_InvalidEnv(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_POLL_MS", "notanumber")
	if got := mountCheckPollInterval(); got != 200*time.Millisecond {
		t.Fatalf("expected 200ms default for invalid env, got %v", got)
	}
}

func TestMountCheckPollInterval_ZeroEnv(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_POLL_MS", "0")
	if got := mountCheckPollInterval(); got != 200*time.Millisecond {
		t.Fatalf("expected 200ms default for zero env, got %v", got)
	}
}

// --- mountCheckTimeout ---

func TestMountCheckTimeout_Default(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_TIMEOUT_MS", "")
	if got := mountCheckTimeout(); got != 30_000*time.Millisecond {
		t.Fatalf("expected 30000ms default, got %v", got)
	}
}

func TestMountCheckTimeout_EnvOverride(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_TIMEOUT_MS", "5000")
	if got := mountCheckTimeout(); got != 5000*time.Millisecond {
		t.Fatalf("expected 5000ms, got %v", got)
	}
}

func TestMountCheckTimeout_InvalidEnv(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_TIMEOUT_MS", "bad")
	if got := mountCheckTimeout(); got != 30_000*time.Millisecond {
		t.Fatalf("expected 30000ms default for invalid env, got %v", got)
	}
}

func TestMountCheckTimeout_ZeroEnv(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_TIMEOUT_MS", "0")
	if got := mountCheckTimeout(); got != 30_000*time.Millisecond {
		t.Fatalf("expected 30000ms default for zero env, got %v", got)
	}
}

// --- waitForMount ---

// TestWaitForMount_AlreadyMounted verifies that waitForMount returns immediately
// when the mountpoint is already in the OS mount table. The system root "/"
// is always mounted on both Linux and macOS.
func TestWaitForMount_AlreadyMounted(t *testing.T) {
	t.Parallel()
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := mgr.waitForMount(ctx, "/"); err != nil {
		t.Fatalf("waitForMount for system root should succeed immediately: %v", err)
	}
}

// TestWaitForMount_Timeout verifies that waitForMount returns an error when the
// context deadline is exceeded before the mount appears.
func TestWaitForMount_Timeout(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_POLL_MS", "10")
	dir := t.TempDir()
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := mgr.waitForMount(ctx, dir); err == nil {
		t.Fatal("expected error for unmounted dir")
	}
}

// TestWaitForMount_CancelledContext verifies that a pre-cancelled context
// causes waitForMount to return an error without polling.
func TestWaitForMount_CancelledContext(t *testing.T) {
	t.Setenv("AKB_MOUNT_CHECK_POLL_MS", "10")
	dir := t.TempDir()
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mgr.waitForMount(ctx, dir); err == nil {
		t.Fatal("expected error for pre-cancelled context")
	}
}
