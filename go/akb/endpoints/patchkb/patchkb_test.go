package patchkb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bkosm/akb/go/akb/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubConfigurer struct {
	cfg         config.Config
	saved       *config.Config
	retrieveErr error
	saveErr     error
}

func (s *stubConfigurer) Retrieve(context.Context) (config.Config, error) {
	return s.cfg, s.retrieveErr
}
func (s *stubConfigurer) Save(_ context.Context, cfg config.Config) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = &cfg
	return nil
}

func strPtr(s string) *string { return &s }

func TestEditKB_Description(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: "/tmp/kb", Description: "old"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:        "my-kb",
		Description: strPtr("new desc"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Hint, "restart") {
		t.Fatalf("hint = %q", out.Hint)
	}
	if sc.saved == nil {
		t.Fatal("Save not called")
	}
	got := sc.saved.KBs["my-kb"]
	if got.Description != "new desc" {
		t.Fatalf("description = %q", got.Description)
	}
	if got.Mount != "/tmp/kb" {
		t.Fatal("existing mount should be preserved")
	}
}

func TestEditKB_RcloneRemote(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: "/tmp/kb", RcloneRemote: ":s3,env_auth=true:old-bucket/"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:         "my-kb",
		RcloneRemote: strPtr(":s3,env_auth=true:new-bucket/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.saved.KBs["my-kb"].RcloneRemote != ":s3,env_auth=true:new-bucket/" {
		t.Fatalf("rclone_remote = %q", sc.saved.KBs["my-kb"].RcloneRemote)
	}
	if sc.saved.KBs["my-kb"].Mount != "/tmp/kb" {
		t.Fatal("existing mount should be preserved")
	}
}

func TestEditKB_Mount(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: "/old/path"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "my-kb",
		Mount: strPtr("/new/path"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.saved.KBs["my-kb"].Mount != "/new/path" {
		t.Fatalf("mount = %q", sc.saved.KBs["my-kb"].Mount)
	}
}

func TestEditKB_Method(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: "/tmp/kb", RcloneRemote: ":s3:bucket/", Method: ""},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:   "my-kb",
		Method: strPtr("nfs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.saved.KBs["my-kb"].Method != "nfs" {
		t.Fatalf("mount_method = %q, want nfs", sc.saved.KBs["my-kb"].Method)
	}
	if sc.saved.KBs["my-kb"].RcloneRemote != ":s3:bucket/" {
		t.Fatal("existing rclone_remote should be preserved")
	}
}

func TestEditKB_RcloneArgs(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: "/tmp/kb", RcloneArgs: map[string]string{"vfs-cache-mode": "full"}},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:       "my-kb",
		RcloneArgs: map[string]string{"buffer-size": "128M"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := sc.saved.KBs["my-kb"].RcloneArgs
	if got["buffer-size"] != "128M" {
		t.Fatalf("rclone_args[buffer-size] = %q, want 128M", got["buffer-size"])
	}
	if _, old := got["vfs-cache-mode"]; old {
		t.Fatal("rclone_args should be fully replaced, old key should be gone")
	}
}

func TestEditKB_SwitchToLocal(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: "/tmp/kb", RcloneRemote: ":s3,env_auth=true:bucket/"},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	empty := ""
	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:         "my-kb",
		RcloneRemote: &empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.saved.KBs["my-kb"].RcloneRemote != "" {
		t.Fatalf("rclone_remote should be empty, got %q", sc.saved.KBs["my-kb"].RcloneRemote)
	}
}

func TestEditKB_NotFound(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:        "nope",
		Description: strPtr("x"),
	})
	if err == nil {
		t.Fatal("expected error for missing kb")
	}
}

func TestEditKB_EmptyName(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{},
	}}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:        "",
		Description: strPtr("x"),
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestHandle_NoConfigInContext(t *testing.T) {
	t.Parallel()
	_, _, err := Handle(context.Background(), &mcp.CallToolRequest{}, Input{
		Name:        "my-kb",
		Description: strPtr("x"),
	})
	if err == nil {
		t.Fatal("expected error when config not in context")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("error = %q, want mention of 'config'", err)
	}
}

func TestHandle_RetrieveError(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{retrieveErr: errors.New("storage unavailable")}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:        "any-kb",
		Description: strPtr("x"),
	})
	if err == nil {
		t.Fatal("expected error when Retrieve fails")
	}
	if !strings.Contains(err.Error(), "retrieve config") {
		t.Fatalf("error = %q, want mention of 'retrieve config'", err)
	}
}

func TestHandle_SaveError(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"my-kb": {Mount: "/tmp/kb"},
			},
		},
		saveErr: errors.New("conflict"),
	}
	ctx := config.IntoContext(context.Background(), sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:        "my-kb",
		Description: strPtr("new desc"),
	})
	if err == nil {
		t.Fatal("expected error when Save fails")
	}
	if !strings.Contains(err.Error(), "save config") {
		t.Fatalf("error = %q, want mention of 'save config'", err)
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}
