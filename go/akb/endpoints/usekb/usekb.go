package usekb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bkosm/akb/go/akb/config"
	"github.com/bkosm/akb/go/akb/endpoints"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Input holds the parameters for the use_kb tool.
type Input struct {
	Name   string `json:"name" jsonschema:"name of the knowledge base to mount, unmount, or sync"`
	Action string `json:"action" jsonschema:"'mount' to activate the KB, 'unmount' to deactivate it, or 'sync' to wait for remote write-back"`
}

// Output is the response payload for the use_kb tool.
type Output struct {
	Mount     string `json:"mount" jsonschema:"resolved local path"`
	Status    string `json:"status" jsonschema:"result of the action"`
	Assurance string `json:"assurance,omitempty" jsonschema:"sync assurance level, present for sync actions"`
	Waited    string `json:"waited,omitempty" jsonschema:"duration waited for write-back, present for sync actions"`
}

// Handle implements the use_kb tool handler.
func Handle(ctx context.Context, req *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	if input.Name == "" {
		return nil, Output{}, fmt.Errorf("name is required")
	}
	if input.Action != "mount" && input.Action != "unmount" && input.Action != "sync" {
		return nil, Output{}, fmt.Errorf("action must be 'mount', 'unmount', or 'sync'")
	}

	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("retrieve config: %w", err)
	}

	kbEntry, ok := cfg.KBs[config.Unique(input.Name)]
	if !ok {
		return nil, Output{}, fmt.Errorf("knowledge base %q not found in config", input.Name)
	}

	resolved := filepath.Clean(os.ExpandEnv(kbEntry.Mount))

	if kbEntry.RcloneRemote == "" {
		if input.Action == "sync" {
			return nil, Output{
				Mount:     resolved,
				Status:    fmt.Sprintf("local KB %q — no sync action needed", input.Name),
				Assurance: mount.SyncAssuranceLocalNoop,
				Waited:    "0s",
			}, nil
		}
		return nil, Output{
			Mount:  resolved,
			Status: fmt.Sprintf("local KB %q — no mount action needed", input.Name),
		}, nil
	}

	mgr, err := mount.ManagerFromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("mount manager: %w", err)
	}

	switch input.Action {
	case "mount":
		if mgr.IsMounted(kbEntry.Mount) {
			return nil, Output{
				Mount:  resolved,
				Status: fmt.Sprintf("KB %q is already mounted at %s", input.Name, resolved),
			}, nil
		}

		if err := mgr.Preflight(); err != nil {
			return nil, Output{}, err
		}
		if err := mgr.Add(ctx, input.Name, kbEntry.RcloneRemote, kbEntry.Mount, mount.Method(kbEntry.Method), kbEntry.RcloneArgs); err != nil {
			return nil, Output{}, fmt.Errorf("mount: %w", err)
		}
		return nil, Output{
			Mount:  resolved,
			Status: fmt.Sprintf("KB %q mounted at %s", input.Name, resolved),
		}, nil

	case "unmount":
		unmountErr := mgr.Unmount(kbEntry.Mount)
		mgr.Deregister(kbEntry.Mount)
		if unmountErr != nil {
			return nil, Output{}, fmt.Errorf("unmount: %w", unmountErr)
		}
		return nil, Output{
			Mount:  resolved,
			Status: fmt.Sprintf("KB %q unmounted from %s", input.Name, resolved),
		}, nil

	case "sync":
		result, err := mgr.Sync(kbEntry.Mount)
		if err != nil {
			return nil, Output{}, fmt.Errorf("sync: %w", err)
		}
		return nil, Output{
			Mount:     resolved,
			Status:    fmt.Sprintf("KB %q sync completed at %s", input.Name, resolved),
			Assurance: result.Assurance,
			Waited:    result.Waited.String(),
		}, nil
	}

	return nil, Output{}, fmt.Errorf("unexpected action %q", input.Action)
}

const toolDescription = `Manually mount or unmount a knowledge base. This is a troubleshooting tool — all KBs are auto-mounted at server startup, so you should not need this in normal operation.

Use cases:
  - Re-mount a remote KB that failed during startup
  - Unmount a remote KB to free system resources (rclone process, FUSE/NFS mount)

Actions:
  - "mount": activate a remote KB (starts rclone, makes files accessible at mount path)
  - "unmount": deactivate a remote KB (stops rclone, releases mountpoint)
  - "sync": after writing to a remote KB, wait for rclone's configured write-back window and verify the mount process remains healthy

Local KBs (no rclone_remote) are always accessible — all actions are no-ops.`

// Register adds the use_kb tool to the MCP server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "use_kb",
		Title:       "Mount / Unmount Knowledge Base",
		Description: toolDescription,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &endpoints.BoolFalse,
			IdempotentHint:  true,
			OpenWorldHint:   &endpoints.BoolFalse,
		},
	}, Handle)
	return nil
}
