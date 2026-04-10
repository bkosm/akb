package newkb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bkosm/akb/config"
	"github.com/bkosm/akb/endpoints"
	"github.com/bkosm/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Input holds the parameters for the new_kb tool.
type Input struct {
	Name         string            `json:"name" jsonschema:"the unique name for this knowledge base"`
	RcloneRemote string            `json:"rclone_remote,omitempty" jsonschema:"rclone remote path spec. Omit for a plain local directory. Format ':backend,opt=val:bucket/path'. Examples: ':s3,env_auth=true,region=us-east-1:my-bucket/prefix/'. See https://rclone.org/overview/#syntax-of-remote-paths"`
	Mount        string            `json:"mount" jsonschema:"local path. When rclone_remote is set this is the FUSE mountpoint (defaults to $HOME/.akb/mounts/<name>). When rclone_remote is omitted this is the existing local directory to use."`
	MountMethod  string            `json:"mount_method,omitempty" jsonschema:"how to mount the remote: 'fuse' (rclone mount, requires macFUSE/FUSE-T/fuse3), 'nfs' (rclone nfsmount, no FUSE needed), or omit for auto (prefer FUSE, fall back to NFS). Ignored for local directories."`
	RcloneArgs   map[string]string `json:"rclone_args,omitempty" jsonschema:"rclone flag overrides keyed by flag name without '--'. Merged on top of defaults (vfs-cache-mode=full, vfs-cache-max-size=1G, etc). Empty value for boolean flags (e.g. {\"read-only\": \"\"}). See https://rclone.org/commands/rclone_mount/#options"`
	Description  string            `json:"description,omitempty" jsonschema:"human-readable description of the knowledge base"`
}

// Output is the response payload for the new_kb tool.
type Output struct {
	Mount string `json:"mount" jsonschema:"the resolved local path to use with file tools"`
	Hint  string `json:"hint" jsonschema:"a hint to the user"`
}

// Handle implements the new_kb tool handler.
func Handle(ctx context.Context, req *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	if input.Name == "" {
		return nil, Output{}, fmt.Errorf("name is required")
	}
	if input.Mount == "" && input.RcloneRemote == "" {
		return nil, Output{}, fmt.Errorf("mount is required (local directory path or FUSE mountpoint)")
	}

	if input.Mount == "" {
		input.Mount = filepath.Join("$HOME", ".akb", "mounts", input.Name)
	}

	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("retrieve config: %w", err)
	}

	if cfg.KBs == nil {
		cfg.KBs = make(map[config.Unique]config.KB)
	}

	unique := config.Unique(input.Name)
	if _, exists := cfg.KBs[unique]; exists {
		return nil, Output{}, fmt.Errorf("knowledge base %q already exists", input.Name)
	}

	resolved := filepath.Clean(os.ExpandEnv(input.Mount))

	mgr, err := mount.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("mount manager: %w", err)
	}

	if input.RcloneRemote != "" {
		if pfErr := mgr.Preflight(); pfErr != nil {
			return nil, Output{}, pfErr
		}
	}
	if addErr := mgr.Add(input.RcloneRemote, input.Mount, mount.MountMethod(input.MountMethod), input.RcloneArgs); addErr != nil {
		return nil, Output{}, fmt.Errorf("mount: %w", addErr)
	}

	cfg.KBs[unique] = config.KB{
		RcloneRemote: input.RcloneRemote,
		Mount:        input.Mount,
		MountMethod:  input.MountMethod,
		RcloneArgs:   input.RcloneArgs,
		Description:  input.Description,
	}

	if err := configurer.Save(ctx, cfg); err != nil {
		// Best-effort cleanup: deregister the mount so a retry can succeed.
		mgr.Deregister(input.Mount)
		return nil, Output{}, fmt.Errorf("save config: %w", err)
	}

	hint := fmt.Sprintf("Knowledge base %q created. Use file tools on %s to read/write documents.", input.Name, resolved)
	if input.RcloneRemote != "" {
		hint += " Backed by rclone remote."
	}

	return nil, Output{Mount: resolved, Hint: hint}, nil
}

const toolDescription = `Register and mount a new knowledge base for cross-repo or cross-host knowledge sharing.

The new KB is persisted to config and mounted immediately. After creation, use standard file tools (Read, Write, Glob, Grep) on the returned mount path.

Two modes:
  1. Remote (rclone_remote set): mounts remote storage as a local directory via rclone.
  2. Local (rclone_remote omitted): uses an existing local directory directly.

mount_method (remote only, optional):
  - omit/auto: prefer FUSE mount, fall back to NFS if FUSE unavailable
  - "fuse": rclone mount (requires macFUSE/FUSE-T on macOS or fuse3 on Linux)
  - "nfs": rclone nfsmount (no FUSE dependency, works everywhere)

rclone_remote format: :backend,opt=val:bucket/path
Common backends:
  - S3 (with env auth):  :s3,env_auth=true,region=us-east-1:my-bucket/prefix/
  - S3 (with keys):      :s3,access_key_id=X,secret_access_key=Y:bucket/prefix/
  - SFTP:                :sftp,host=example.com,user=me:/path

Full backend list: https://rclone.org/overview/#supported-providers`

// Register adds the new_kb tool to the MCP server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "new_kb",
		Title:       "Create Knowledge Base",
		Description: toolDescription,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &endpoints.BoolFalse,
			OpenWorldHint:   &endpoints.BoolFalse,
		},
	}, Handle)
	return nil
}
