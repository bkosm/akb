package kbs

import (
	"context"
	"encoding/json"
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

func parseKBs(t *testing.T, result *mcp.ReadResourceResult) map[string]KBInfo {
	t.Helper()
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
	var out struct {
		KBs map[string]KBInfo `json:"kbs"`
	}
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out.KBs
}

func TestHandler_WithKBs(t *testing.T) {
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

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	kbMap := parseKBs(t, result)
	if len(kbMap) != 2 {
		t.Fatalf("len = %d, want 2", len(kbMap))
	}
	if _, ok := kbMap["alpha"]; !ok {
		t.Fatal("missing key alpha")
	}
	if _, ok := kbMap["beta"]; !ok {
		t.Fatal("missing key beta")
	}
	if kbMap["alpha"].Description != "Alpha KB" {
		t.Fatalf("description = %q", kbMap["alpha"].Description)
	}
}

func TestHandler_Empty(t *testing.T) {
	t.Parallel()
	ctx := config.IntoContext(context.Background(), &stubConfigurer{cfg: config.Config{}})

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	kbMap := parseKBs(t, result)
	if len(kbMap) != 0 {
		t.Fatalf("len = %d, want 0", len(kbMap))
	}
}

func TestHandler_NoConfig(t *testing.T) {
	t.Parallel()
	_, err := handler(context.Background(), &mcp.ReadResourceRequest{})
	if err == nil {
		t.Fatal("expected error when config not in context")
	}
}

func TestHandler_LocalDirMounted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"local": {Mount: dir, Description: "local dir"},
			},
		},
	})

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	kbMap := parseKBs(t, result)
	if len(kbMap) != 1 {
		t.Fatalf("len = %d, want 1", len(kbMap))
	}
	if kbMap["local"].MountStatus != MountStatusMounted {
		t.Fatalf("MountStatus = %q, want %q", kbMap["local"].MountStatus, MountStatusMounted)
	}
}

func TestHandler_NoMountManager(t *testing.T) {
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

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	kbMap := parseKBs(t, result)
	if len(kbMap) != 2 {
		t.Fatalf("len = %d, want 2", len(kbMap))
	}
	if kbMap["local"].MountStatus != MountStatusMounted {
		t.Fatalf("local MountStatus = %q, want %q", kbMap["local"].MountStatus, MountStatusMounted)
	}
	if kbMap["remote"].MountStatus != MountStatusNotMounted {
		t.Fatalf("remote MountStatus = %q, want %q", kbMap["remote"].MountStatus, MountStatusNotMounted)
	}
}

func TestHandler_RemoteKB_MountFailed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mgr := mount.NewManager()
	mgr.SetMountError(dir, fmt.Errorf("rclone: not found"))

	ctx := config.IntoContext(context.Background(), &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"remote-kb": {Mount: dir, RcloneRemote: ":s3:bucket/", Description: "remote"},
			},
		},
	})
	ctx = mount.ManagerIntoContext(ctx, mgr)

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	kbMap := parseKBs(t, result)
	if len(kbMap) != 1 {
		t.Fatalf("len = %d, want 1", len(kbMap))
	}
	kb := kbMap["remote-kb"]
	if kb.MountStatus != MountStatusFailed {
		t.Fatalf("MountStatus = %q, want %q", kb.MountStatus, MountStatusFailed)
	}
	if kb.MountError == "" {
		t.Fatal("MountError should be non-empty")
	}
}

func TestHandler_ResponseShape(t *testing.T) {
	t.Parallel()
	ctx := config.IntoContext(context.Background(), &stubConfigurer{cfg: config.Config{}})

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
	c := result.Contents[0]
	if c.URI != ResourceURI {
		t.Fatalf("URI = %q, want %q", c.URI, ResourceURI)
	}
	if c.MIMEType != "application/json" {
		t.Fatalf("MIMEType = %q", c.MIMEType)
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}
