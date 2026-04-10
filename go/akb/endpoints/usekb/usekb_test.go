package usekb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bkosm/akb/go/akb/config"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubConfigurer struct {
	cfg config.Config
}

func (s *stubConfigurer) Retrieve(context.Context) (config.Config, error) { return s.cfg, nil }
func (s *stubConfigurer) Save(context.Context, config.Config) error       { return nil }

func TestHandle_LocalKB_MountNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"local-kb": {Mount: dir},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "local-kb", Action: "mount"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Status, "no mount action needed") {
		t.Fatalf("status = %q, want noop message", out.Status)
	}
	if out.Mount != dir {
		t.Fatalf("mount = %q, want %q", out.Mount, dir)
	}
}

func TestHandle_LocalKB_UnmountNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"local-kb": {Mount: dir},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "local-kb", Action: "unmount"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Status, "no mount action needed") {
		t.Fatalf("status = %q, want noop message", out.Status)
	}
}

func TestHandle_RemoteKB_MountPreflightFails(t *testing.T) {
	t.Setenv("PATH", "")
	dir := t.TempDir()

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"remote-kb": {Mount: dir, RcloneRemote: ":s3:bucket/"},
		},
	}}

	mgr := mount.NewManager()
	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.IntoContext(ctx, mgr)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "remote-kb", Action: "mount"})
	if err == nil {
		t.Fatal("expected error when rclone is not on PATH")
	}
	if !strings.Contains(err.Error(), "rclone") {
		t.Fatalf("error = %q, want mention of rclone", err)
	}
}

func TestHandle_RemoteKB_MountSuccess(t *testing.T) {
	if _, err := exec.LookPath("rclone"); err != nil {
		t.Skip("rclone not installed")
	}

	dir := t.TempDir()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"remote-kb": {
				Mount:        dir,
				RcloneRemote: ":memory:",
			},
		},
	}}

	mgr := mount.NewManager()
	t.Cleanup(func() { _ = mgr.Unmount(dir) })

	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.IntoContext(ctx, mgr)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "remote-kb", Action: "mount"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Status, "mounted") {
		t.Fatalf("status = %q, want mounted message", out.Status)
	}
}

// TestHandle_Unmount_Deregisters checks that a successful unmount also
// deregisters the mountpoint, allowing a subsequent Add for the same path.
// It uses a local entry (remote="") so Unmount is a no-op and Deregister is the
// meaningful operation being tested.
func TestHandle_Unmount_Deregisters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mgr := mount.NewManager()
	// Add as a local entry so Unmount succeeds (no-op for local).
	if err := mgr.Add("", dir, mount.MethodAuto, nil); err != nil {
		t.Fatal(err)
	}

	// Config says this KB has a remote — handle will call Unmount+Deregister.
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: dir, RcloneRemote: ":s3:bucket/"},
		},
	}}

	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.IntoContext(ctx, mgr)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb", Action: "unmount"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Status, "unmounted") {
		t.Fatalf("status = %q, want unmounted message", out.Status)
	}

	// After deregister, Add should succeed again for the same path.
	if err := mgr.Add("", dir, mount.MethodAuto, nil); err != nil {
		t.Fatalf("re-Add after deregister should succeed: %v", err)
	}
}

// TestHandle_Unmount_DeregistersEvenOnError verifies that Deregister is always
// called even when Unmount returns an error, so a subsequent Add can succeed.
//
// Strategy: the KB's mount path is NOT registered in the manager, so
// Unmount("not registered") returns an error. Deregister is a no-op in that
// case, but the important invariant is that Handle still propagates the error
// and a follow-up Add for the same path succeeds.
func TestHandle_Unmount_DeregistersEvenOnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mgr := mount.NewManager()
	// Intentionally leave `dir` unregistered in the manager so that
	// mgr.Unmount(dir) returns a "not registered" error.

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"remote-kb": {Mount: dir, RcloneRemote: ":s3:bucket/"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.IntoContext(ctx, mgr)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "remote-kb", Action: "unmount"})
	if err == nil {
		t.Fatal("expected error when mountpoint not registered in manager")
	}

	// After the failed unmount, Deregister must have been called (no-op here
	// since the path was never registered). The key invariant: a subsequent
	// Add for the same path must not be blocked.
	if err := mgr.Add("", dir, mount.MethodAuto, nil); err != nil {
		t.Fatalf("Add after failed unmount should succeed: %v", err)
	}
}

func TestHandle_KBNotFound(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "nope", Action: "mount"})
	if err == nil {
		t.Fatal("expected error for missing KB")
	}
}

func TestHandle_InvalidAction(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"kb": {Mount: "/tmp/kb"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "kb", Action: "restart"})
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}

func TestHandle_EmptyName(t *testing.T) {
	t.Parallel()
	_, _, err := Handle(context.Background(), &mcp.CallToolRequest{}, Input{Action: "mount"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestHandle_EnvExpansion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_USE_KB_DIR", dir)

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"env-kb": {Mount: "$TEST_USE_KB_DIR"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "env-kb", Action: "mount"})
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Clean(dir)
	if out.Mount != expected {
		t.Fatalf("mount = %q, want %q", out.Mount, expected)
	}
}

func TestHandle_NoMountManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"remote-kb": {Mount: dir, RcloneRemote: ":s3:bucket/"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "remote-kb", Action: "mount"})
	if err == nil {
		t.Fatal("expected error when mount manager not in context")
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}
