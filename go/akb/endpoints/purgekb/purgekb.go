package purgekb

import (
	"context"
	"fmt"
	"os"

	"github.com/bkosm/akb/go/akb/config"
	"github.com/bkosm/akb/go/akb/endpoints"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Input holds the parameters for the purge_kb tool.
type Input struct {
	Name        string `json:"name"         jsonschema:"config key of the KB to remove"`
	DeleteFiles bool   `json:"delete_files" jsonschema:"if true, recursively delete all files at the KB mount path before removing from config"`
}

// Output is the response payload for the purge_kb tool.
type Output struct {
	Message string `json:"message"`
}

// Handle implements the purge_kb tool handler.
func Handle(ctx context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	if input.Name == "" {
		return nil, Output{}, fmt.Errorf("name is required")
	}

	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("retrieve config: %w", err)
	}

	key := config.Unique(input.Name)
	kb, ok := cfg.KBs[key]
	if !ok {
		return nil, Output{}, fmt.Errorf("kb %q not found", input.Name)
	}

	if input.DeleteFiles {
		// Best-effort unmount before wiping files so mounted FUSE/NFS
		// directories are released first. Errors are intentionally ignored
		// because the KB may not be currently mounted.
		if mgr, mgrErr := mount.ManagerFromContext(ctx); mgrErr == nil {
			_ = mgr.Unmount(kb.Mount)
			mgr.Deregister(kb.Mount)
		}

		if err := os.RemoveAll(kb.Mount); err != nil {
			return nil, Output{}, fmt.Errorf("delete files: %w", err)
		}
	}

	delete(cfg.KBs, key)

	if err := configurer.Save(ctx, cfg); err != nil {
		return nil, Output{}, fmt.Errorf("save config: %w", err)
	}

	return nil, Output{Message: fmt.Sprintf("KB %q removed from config.", input.Name)}, nil
}

// Register adds the purge_kb tool to the MCP server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "purge_kb",
		Title: "Remove Knowledge Base",
		Description: `Remove a knowledge base entry from config.

If delete_files is true, all files at the KB's mount path are deleted recursively before the config entry is removed. Use with caution — this is irreversible.

The KB is immediately removed from config. Any active mount is unmounted on a best-effort basis when delete_files is true.`,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &endpoints.BoolTrue,
			IdempotentHint:  false,
			OpenWorldHint:   &endpoints.BoolFalse,
		},
	}, Handle)
	return nil
}
