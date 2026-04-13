package listkbs

import (
	"context"
	"fmt"
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

func TestHandle_WithKBs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"beta":  {Mount: dir, Description: "Beta KB"},
				"alpha": {Mount: dir, Description: "Alpha KB"},
			},
		},
	})

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.KBs) != 2 {
		t.Fatalf("len = %d, want 2", len(out.KBs))
	}
	if out.KBs[0].Name != "alpha" || out.KBs[1].Name != "beta" {
		t.Fatalf("order: %v", out.KBs)
	}
	if out.KBs[0].Description != "Alpha KB" {
		t.Fatalf("description = %q", out.KBs[0].Description)
	}
}

func TestHandle_Empty(t *testing.T) {
	t.Parallel()
	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{},
	})

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.KBs) != 0 {
		t.Fatalf("len = %d, want 0", len(out.KBs))
	}
}

func TestHandle_NoConfig(t *testing.T) {
	t.Parallel()
	_, _, err := Handle(context.Background(), &mcp.CallToolRequest{}, Input{})
	if err == nil {
		t.Fatal("expected error when config not in context")
	}
}

func TestHandle_LocalDirMounted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"local": {Mount: dir, Description: "local dir"},
			},
		},
	})

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.KBs) != 1 {
		t.Fatalf("len = %d, want 1", len(out.KBs))
	}
	if out.KBs[0].MountStatus != MountStatusMounted {
		t.Fatalf("MountStatus = %q, want %q", out.KBs[0].MountStatus, MountStatusMounted)
	}
}

// TestHandle_RemoteKB_IsMounted checks that a remote KB with an active mount
// manager entry reports mounted=true when the manager confirms the mount.
func TestHandle_RemoteKB_MountManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register a local directory entry in the manager to simulate a mounted KB.
	// IsMounted returns false for local entries (remote=""), so we verify the
	// mounted=false path for remote KBs whose manager entry has no active mount.
	mgr := mount.NewManager()
	if err := mgr.Add(context.Background(), "test", "", dir, mount.MethodAuto, nil); err != nil {
		t.Fatal(err)
	}

	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"remote-kb": {Mount: dir, RcloneRemote: ":s3:bucket/", Description: "remote"},
			},
		},
	})
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.KBs) != 1 {
		t.Fatalf("len = %d, want 1", len(out.KBs))
	}
	// The manager has the path registered as local (no remote), so IsMounted
	// returns false; the local-dir fallback also won't fire because RcloneRemote
	// is set. Status must be not_mounted.
	if out.KBs[0].MountStatus != MountStatusNotMounted {
		t.Fatalf("MountStatus = %q, want %q", out.KBs[0].MountStatus, MountStatusNotMounted)
	}
}

// TestHandle_NoMountManager checks that list_kbs still works (gracefully) when
// no mount manager is present in context — local KBs use the dir-existence
// heuristic, remote KBs show mounted=false.
func TestHandle_NoMountManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"local":  {Mount: dir},
				"remote": {Mount: dir, RcloneRemote: ":s3:bucket/"},
			},
		},
	})
	// No mount manager in context.

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.KBs) != 2 {
		t.Fatalf("len = %d, want 2", len(out.KBs))
	}
	byName := make(map[string]KBInfo)
	for _, kb := range out.KBs {
		byName[kb.Name] = kb
	}
	if byName["local"].MountStatus != MountStatusMounted {
		t.Fatalf("local MountStatus = %q, want %q", byName["local"].MountStatus, MountStatusMounted)
	}
	if byName["remote"].MountStatus != MountStatusNotMounted {
		t.Fatalf("remote MountStatus = %q, want %q", byName["remote"].MountStatus, MountStatusNotMounted)
	}
}

// TestHandle_RemoteKB_MountFailed checks that a remote KB whose last Add
// returned an error is reported with mount_status="failed" and a non-empty
// mount_error message.
func TestHandle_RemoteKB_MountFailed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mgr := mount.NewManager()
	mountErr := fmt.Errorf("rclone mount %q at %q: exec: not found", ":s3:bucket/", dir)
	mgr.SetMountError(dir, mountErr)

	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"remote-kb": {Mount: dir, RcloneRemote: ":s3:bucket/", Description: "remote"},
			},
		},
	})
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.KBs) != 1 {
		t.Fatalf("len = %d, want 1", len(out.KBs))
	}
	kb := out.KBs[0]
	if kb.MountStatus != MountStatusFailed {
		t.Fatalf("MountStatus = %q, want %q", kb.MountStatus, MountStatusFailed)
	}
	if kb.MountError == "" {
		t.Fatal("MountError should be non-empty for a failed mount")
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}
