package listkbs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/bkosm/akb/config"
	"github.com/bkosm/akb/endpoints"
	"github.com/bkosm/akb/mount"
)

type Input struct{}

type KBInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Mount       string `json:"mount"`
	MountMethod string `json:"mount_method,omitempty"`
	Mounted     bool   `json:"mounted"`
}

type Output struct {
	KBs []KBInfo `json:"kbs"`
}

func Handle(ctx context.Context, req *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("retrieve config: %w", err)
	}

	mgr, _ := mount.FromContext(ctx)

	kbs := make([]KBInfo, 0, len(cfg.KBs))
	for name, entry := range cfg.KBs {
		resolved := filepath.Clean(os.ExpandEnv(entry.Mount))
		mounted := false
		if mgr != nil {
			mounted = mgr.IsMounted(entry.Mount)
		}
		if !mounted && entry.RcloneRemote == "" {
			if fi, err := os.Stat(resolved); err == nil && fi.IsDir() {
				mounted = true
			}
		}
		kbs = append(kbs, KBInfo{
			Name:        string(name),
			Description: entry.Description,
			Mount:       resolved,
			MountMethod: entry.MountMethod,
			Mounted:     mounted,
		})
	}
	sort.Slice(kbs, func(i, j int) bool { return kbs[i].Name < kbs[j].Name })

	return nil, Output{KBs: kbs}, nil
}

var boolFalse = false

var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "list_kbs",
		Title: "List Knowledge Bases",
		Description: `List all configured knowledge bases with their mount paths and status.

Each KB entry includes a mount path and a mounted flag. All KBs are auto-mounted at server startup, so mounted KBs are ready for immediate use with standard file tools (Read, Write, Glob, Grep) on the mount path.

If a KB shows mounted=false, it may have failed to mount at startup — use use_kb to retry.`,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			OpenWorldHint:   &boolFalse,
			DestructiveHint: &boolFalse,
		},
	}, Handle)
	return nil
}
