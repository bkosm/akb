// Package promptreference registers the akb://prompt-reference MCP resource,
// which provides agents with the full authoring reference for .prompt.md files.
package promptreference

import (
	"context"
	_ "embed"

	"github.com/bkosm/akb/go/akb/endpoints"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resourceURI = "akb://prompt-reference"

//go:embed reference.md
var referenceText string

func handler(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      resourceURI,
			MIMEType: "text/markdown",
			Text:     referenceText,
		}},
	}, nil
}

// Register adds the akb://prompt-reference MCP resource to the server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	s.AddResource(&mcp.Resource{
		URI:         resourceURI,
		Name:        "prompt-reference",
		Title:       "AKB Prompt Authoring Reference",
		Description: "Full reference for authoring .prompt.md files: naming, frontmatter schema, single- and multi-message format, Go template syntax, and the include function.",
		MIMEType:    "text/markdown",
		Annotations: &mcp.Annotations{
			Audience: []mcp.Role{"assistant"},
			Priority: 0.7,
		},
	}, handler)
	return nil
}
