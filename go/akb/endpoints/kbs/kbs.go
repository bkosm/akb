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
	config.KB
	ResolvedMountPath   string                          `json:"resolved_mount_path"`
	MountStatus         MountStatus                     `json:"mount_status"`
	MountError          string                          `json:"mount_error,omitempty"`
	ResolvedMountMethod string                          `json:"resolved_mount_method,omitempty"`
	MountDetails        *mount.MountDetails             `json:"mount_details,omitempty"`
	RcloneDurability    *mount.RcloneDurabilitySettings `json:"rclone_durability,omitempty"`
}

type mountState interface {
	IsMounted(string) bool
	MountError(string) error
	ResolvedMethod(string) (mount.Method, bool)
	MountDetails(string) (mount.MountDetails, bool)
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

	var mounts mountState
	if mgr, _ := mount.ManagerFromContext(ctx); mgr != nil {
		mounts = mgr
	}

	kbMap := buildKBMap(cfg, mounts)
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

func buildKBMap(cfg config.Config, mounts mountState) map[string]KBInfo {
	kbMap := make(map[string]KBInfo, len(cfg.KBs))
	for name, entry := range cfg.KBs {
		resolved := filepath.Clean(os.ExpandEnv(entry.Mount))

		status := MountStatusNotMounted
		mountErrMsg := ""
		resolvedMountMethod := ""
		var mountDetails *mount.MountDetails
		var rcloneDurability *mount.RcloneDurabilitySettings

		if entry.RcloneRemote != "" {
			settings, settingsErr := mount.RcloneDurability(entry.RcloneArgs)
			if settingsErr != nil {
				status = MountStatusFailed
				mountErrMsg = settingsErr.Error()
			} else {
				rcloneDurability = &settings
			}
		}

		switch {
		case status == MountStatusFailed:
		case mounts != nil && mounts.IsMounted(entry.Mount):
			status = MountStatusMounted
			if entry.RcloneRemote != "" {
				if method, ok := mounts.ResolvedMethod(entry.Mount); ok {
					resolvedMountMethod = string(method)
				}
				if details, ok := mounts.MountDetails(entry.Mount); ok {
					mountDetails = &details
				}
			}
		case entry.RcloneRemote == "":
			if fi, err := os.Stat(resolved); err == nil && fi.IsDir() {
				status = MountStatusMounted
			}
		case mounts != nil:
			if err := mounts.MountError(entry.Mount); err != nil {
				status = MountStatusFailed
				mountErrMsg = err.Error()
			}
		}

		kbMap[string(name)] = KBInfo{
			KB:                  entry,
			ResolvedMountPath:   resolved,
			MountStatus:         status,
			MountError:          mountErrMsg,
			ResolvedMountMethod: resolvedMountMethod,
			MountDetails:        mountDetails,
			RcloneDurability:    rcloneDurability,
		}
	}
	return kbMap
}

// Register adds the akb://kbs MCP resource to the server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	s.AddResource(&mcp.Resource{
		URI:   ResourceURI,
		Name:  "kbs",
		Title: "Knowledge Bases",
		Description: `Knowledge bases with config, resolved mount paths, and live mount status (name as key):
  - resolved_mount_path: expanded, absolute path to use with file tools (Read, Write, Glob, Grep)
  - mount_status: "mounted", "not_mounted", or "failed" — live state from the mount manager
  - mount_error: present only when mount_status is "failed"
  - resolved_mount_method: concrete remote mount method selected by the mount manager ("fuse" or "nfs")
  - mount_details: concrete rclone subcommand plus best-effort FUSE and OS mount-table diagnostics
  - rclone_durability: effective remote write-back/backsync timings for remote KBs

Only "mounted" KBs are ready for use; "not_mounted" means startup is still in progress.`,
		MIMEType: "application/json",
		Annotations: &mcp.Annotations{
			Audience: []mcp.Role{"assistant"},
			Priority: 0.9,
		},
	}, handler)
	return nil
}
