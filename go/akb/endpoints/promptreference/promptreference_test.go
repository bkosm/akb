package promptreference

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegister(t *testing.T) {
	t.Parallel()
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	if err := Register(context.Background(), server); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestHandler_ReturnsMarkdown(t *testing.T) {
	t.Parallel()
	result, err := handler(context.Background(), &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
	c := result.Contents[0]
	if c.URI != resourceURI {
		t.Errorf("URI = %q, want %q", c.URI, resourceURI)
	}
	if c.MIMEType != "text/markdown" {
		t.Errorf("MIMEType = %q, want text/markdown", c.MIMEType)
	}
	if strings.TrimSpace(c.Text) == "" {
		t.Error("content text is empty")
	}
	for _, section := range []string{"Naming", "frontmatter", "Single-message", "Template syntax", "include"} {
		if !strings.Contains(c.Text, section) {
			t.Errorf("content missing expected section %q", section)
		}
	}
}
