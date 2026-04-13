package mount

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
