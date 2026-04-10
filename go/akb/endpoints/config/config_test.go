package config

import (
	"context"
	"encoding/json"
	"testing"

	akbconfig "github.com/bkosm/akb/go/akb/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubConfigurer struct {
	cfg akbconfig.Config
}

func (s *stubConfigurer) Retrieve(context.Context) (akbconfig.Config, error) { return s.cfg, nil }
func (s *stubConfigurer) Save(context.Context, akbconfig.Config) error       { return nil }

func TestHandler_returnsConfigJSON(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: akbconfig.Config{
		KBs: map[akbconfig.Unique]akbconfig.KB{
			"docs": {Mount: "/tmp/docs", Description: "documentation"},
		},
	}}
	ctx := akbconfig.IntoContext(context.Background(), sc)

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
	c := result.Contents[0]
	if c.URI != resourceURI {
		t.Fatalf("URI = %q, want %q", c.URI, resourceURI)
	}
	if c.MIMEType != "application/json" {
		t.Fatalf("MIMEType = %q", c.MIMEType)
	}

	var parsed akbconfig.Config
	if err := json.Unmarshal([]byte(c.Text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := parsed.KBs["docs"]; !ok {
		t.Fatal("expected docs KB in parsed config")
	}
}

func TestHandler_emptyConfig(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: akbconfig.Config{}}
	ctx := akbconfig.IntoContext(context.Background(), sc)

	result, err := handler(ctx, &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
	if result.Contents[0].Text == "" {
		t.Fatal("expected non-empty JSON text")
	}
}

func TestHandler_noConfigInContext(t *testing.T) {
	t.Parallel()
	_, err := handler(context.Background(), &mcp.ReadResourceRequest{})
	if err == nil {
		t.Fatal("expected error when config not in context")
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}
