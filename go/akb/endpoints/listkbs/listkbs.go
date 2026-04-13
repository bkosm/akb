package listkbs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bkosm/akb/go/akb/config"
	"github.com/bkosm/akb/go/akb/endpoints"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Input holds the parameters for the list_kbs tool (none required).
type Input struct{}

// MountStatus describes the mount state of a knowledge base.
type MountStatus string

const (
	MountStatusMounted    MountStatus = "mounted"
	MountStatusNotMounted MountStatus = "not_mounted"
	MountStatusFailed     MountStatus = "failed"
)

// KBInfo describes a single knowledge base entry in the list response.
type KBInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Mount       string      `json:"mount"`
	Method      string      `json:"mount_method,omitempty"`
	MountStatus MountStatus `json:"mount_status"`
	MountError  string      `json:"mount_error,omitempty"`
}

// Output is the response payload for the list_kbs tool.
type Output struct {
	KBs []KBInfo `json:"kbs"`
}

// Handle implements the list_kbs tool handler.
func Handle(ctx context.Context, req *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("retrieve config: %w", err)
	}

	mgr, _ := mount.ManagerFromContext(ctx)

	kbs := make([]KBInfo, 0, len(cfg.KBs))
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

		kbs = append(kbs, KBInfo{
			Name:        string(name),
			Description: entry.Description,
			Mount:       resolved,
			Method:      entry.Method,
			MountStatus: status,
			MountError:  mountErrMsg,
		})
	}
	sort.Slice(kbs, func(i, j int) bool { return kbs[i].Name < kbs[j].Name })

	return nil, Output{KBs: kbs}, nil
}

// Register adds the list_kbs tool to the MCP server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "list_kbs",
		Title: "List Knowledge Bases",
		Description: `List all configured knowledge bases with their mount paths and status.

Each KB entry includes a mount path and a mount_status field ("mounted", "not_mounted", or "failed"). All KBs are auto-mounted at server startup, so mounted KBs are ready for immediate use with standard file tools (Read, Write, Glob, Grep) on the mount path.

If a KB shows mount_status="failed", the mount_error field contains the reason — use use_kb to retry.`,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			OpenWorldHint:   &endpoints.BoolFalse,
			DestructiveHint: &endpoints.BoolFalse,
		},
	}, Handle)
	return nil
}
