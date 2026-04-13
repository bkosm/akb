package purgekb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
		// Delete the contents of the mount path (not the root directory itself)
		// so that FUSE/NFS-mounted remote KBs propagate each deletion to the
		// remote. os.RemoveAll on the mountpoint root fails while mounted
		// ("resource busy"), but removing individual entries inside it works.
		entries, err := os.ReadDir(kb.Mount)
		if err != nil && !os.IsNotExist(err) {
			return nil, Output{}, fmt.Errorf("delete files: %w", err)
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(kb.Mount, e.Name())); err != nil {
				return nil, Output{}, fmt.Errorf("delete files: %w", err)
			}
		}
		// For remote KBs, give rclone's VFS write-back cache time to flush
		// the deletions to the remote before the mount process is killed.
		// Local KBs have no cache to flush.
		if kb.RcloneRemote != "" {
			time.Sleep(5 * time.Second)
		}
	}

	// Always unmount and deregister, regardless of delete_files.
	// Errors are intentionally ignored — the KB may not be currently mounted.
	if mgr, mgrErr := mount.ManagerFromContext(ctx); mgrErr == nil {
		_ = mgr.Unmount(kb.Mount)
		mgr.Deregister(kb.Mount)
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

Any active mount is always unmounted on a best-effort basis.

If delete_files is true, all files at the mount path are deleted first (through the live mount, so deletions propagate to the remote), followed by a short flush wait before the mount is torn down. Use with caution — this is irreversible.`,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &endpoints.BoolTrue,
			IdempotentHint:  false,
			OpenWorldHint:   &endpoints.BoolFalse,
		},
	}, Handle)
	return nil
}
