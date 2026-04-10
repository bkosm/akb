package config

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	akbconfig "github.com/bkosm/akb/config"
	"github.com/bkosm/akb/endpoints"
)

const resourceURI = "akb://config"

func handler(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	configurer, err := akbconfig.FromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieve config: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      resourceURI,
			MIMEType: "application/json",
			Text:     string(data),
		}},
	}, nil
}

// Register adds the akb://config MCP resource to the server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	s.AddResource(&mcp.Resource{
		URI:         resourceURI,
		Name:        "config",
		Title:       "AKB Configuration",
		Description: "Full AKB configuration including all registered knowledge bases, their storage backends and mount paths.",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{
			Audience: []mcp.Role{"assistant"},
			Priority: 0.5,
		},
	}, handler)
	return nil
}
