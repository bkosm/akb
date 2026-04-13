package kbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bkosm/akb/go/akb/config"
	"github.com/bkosm/akb/go/akb/endpoints"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ResourceURI is the MCP resource URI for the KB list.
const ResourceURI = "akb://kbs"

// MountStatus describes the mount state of a knowledge base.
type MountStatus string

const (
	MountStatusMounted    MountStatus = "mounted"
	MountStatusNotMounted MountStatus = "not_mounted"
	MountStatusFailed     MountStatus = "failed"
)

// KBInfo describes a single knowledge base entry.
type KBInfo struct {
	Description  string      `json:"description,omitempty"`
	Mount        string      `json:"mount"`
	Method       string      `json:"mount_method,omitempty"`
	RcloneRemote string      `json:"rclone_remote,omitempty"`
	MountStatus  MountStatus `json:"mount_status"`
	MountError   string      `json:"mount_error,omitempty"`
}

func handler(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieve config: %w", err)
	}

	mgr, _ := mount.ManagerFromContext(ctx)

	kbMap := make(map[string]KBInfo, len(cfg.KBs))
	for name, entry := range cfg.KBs {
		resolved := filepath.Clean(os.ExpandEnv(entry.Mount))

		status := MountStatusNotMounted
		mountErrMsg := ""

		switch {
		case mgr != nil && mgr.IsMounted(entry.Mount):
			status = MountStatusMounted
		case entry.RcloneRemote == "":
			if fi, err := os.Stat(resolved); err == nil && fi.IsDir() {
				status = MountStatusMounted
			}
		case mgr != nil:
			if err := mgr.MountError(entry.Mount); err != nil {
				status = MountStatusFailed
				mountErrMsg = err.Error()
			}
		}

		kbMap[string(name)] = KBInfo{
			Description:  entry.Description,
			Mount:        resolved,
			Method:       entry.Method,
			RcloneRemote: entry.RcloneRemote,
			MountStatus:  status,
			MountError:   mountErrMsg,
		}
	}

	data, err := json.MarshalIndent(struct {
		KBs map[string]KBInfo `json:"kbs"`
	}{KBs: kbMap}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal kbs: %w", err)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      ResourceURI,
			MIMEType: "application/json",
			Text:     string(data),
		}},
	}, nil
}

// Register adds the akb://kbs MCP resource to the server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	s.AddResource(&mcp.Resource{
		URI:   ResourceURI,
		Name:  "kbs",
		Title: "Knowledge Bases",
		Description: `Knowledge bases with live mount status. Same map shape as akb://config (name as key), extended with:
  - mount_status: "mounted", "not_mounted", or "failed" — live state from the mount manager
  - mount_error: present only when mount_status is "failed"

Use mount as the local path for file tools (Read, Write, Glob, Grep).
Only "mounted" KBs are ready for use; "not_mounted" means startup is still in progress.`,
		MIMEType: "application/json",
		Annotations: &mcp.Annotations{
			Audience: []mcp.Role{"assistant"},
			Priority: 0.9,
		},
	}, handler)
	return nil
}
