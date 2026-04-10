package patchconfig

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/bkosm/akb/config"
	"github.com/bkosm/akb/endpoints"
)

// Input holds the parameters for the patch_config tool.
type Input struct {
	Name         string            `json:"name" jsonschema:"the config key of the KB entry to edit"`
	RcloneRemote *string           `json:"rclone_remote,omitempty" jsonschema:"new rclone remote path spec. Set empty string to switch to plain local mode. Format ':backend,opt=val:bucket/path'. See https://rclone.org/overview/#syntax-of-remote-paths"`
	Mount        *string           `json:"mount,omitempty" jsonschema:"new local mount path. Env vars allowed (e.g. $HOME/.akb/mounts/my-kb)"`
	MountMethod  *string           `json:"mount_method,omitempty" jsonschema:"mount strategy: 'fuse' (requires macFUSE/FUSE-T/fuse3), 'nfs' (rclone nfsmount, no FUSE), or empty string for auto. Ignored for local directories."`
	RcloneArgs   map[string]string `json:"rclone_args,omitempty" jsonschema:"rclone flag overrides keyed by flag name without '--'. Replaces the entire rclone_args map when provided. See https://rclone.org/commands/rclone_mount/#options"`
	Description  *string           `json:"description,omitempty" jsonschema:"new description for the KB"`
}

// Output is the response payload for the patch_config tool.
type Output struct {
	Hint string `json:"hint"`
}

const savedHint = "Config saved. MCP server restart required for changes to take effect."

// Handle implements the patch_config tool handler.
func Handle(ctx context.Context, req *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("retrieve config: %w", err)
	}

	if err := editKB(&cfg, input); err != nil {
		return nil, Output{}, err
	}

	if err := configurer.Save(ctx, cfg); err != nil {
		return nil, Output{}, fmt.Errorf("save config: %w", err)
	}

	return nil, Output{Hint: savedHint}, nil
}

func editKB(cfg *config.Config, input Input) error {
	key := config.Unique(input.Name)
	entry, ok := cfg.KBs[key]
	if !ok {
		return fmt.Errorf("kb %q not found", input.Name)
	}

	if input.Description != nil {
		entry.Description = *input.Description
	}
	if input.RcloneRemote != nil {
		entry.RcloneRemote = *input.RcloneRemote
	}
	if input.Mount != nil {
		entry.Mount = *input.Mount
	}
	if input.MountMethod != nil {
		entry.MountMethod = *input.MountMethod
	}
	if input.RcloneArgs != nil {
		entry.RcloneArgs = input.RcloneArgs
	}

	cfg.KBs[key] = entry
	return nil
}

// Register adds the patch_config tool to the MCP server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "patch_config",
		Title: "Update Configuration",
		Description: `Merge-patch a knowledge base config entry.

Only provided fields are updated; omitted fields are preserved. Works for KB settings: rclone_remote, mount path, mount method, rclone args, description.

Changes are persisted immediately but take full effect after MCP server restart.`,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &endpoints.BoolFalse,
			IdempotentHint:  true,
			OpenWorldHint:   &endpoints.BoolFalse,
		},
	}, Handle)
	return nil
}
