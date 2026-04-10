package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textOf(t *testing.T, msg *mcp.PromptMessage) string {
	t.Helper()
	tc, ok := msg.Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content is %T, want *mcp.TextContent", msg.Content)
	}
	return tc.Text
}

func TestRender_simpleSubstitution(t *testing.T) {
	t.Parallel()
	def := Definition{
		Messages: []Message{
			{Role: "user", Content: "Review this {{.language}} code."},
		},
	}
	msgs, err := Render(def, map[string]string{"language": "Go"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d", len(msgs))
	}
	if got := textOf(t, msgs[0]); got != "Review this Go code." {
		t.Fatalf("got %q", got)
	}
}

func TestRender_conditionalBlock(t *testing.T) {
	t.Parallel()
	def := Definition{
		Messages: []Message{
			{Role: "user", Content: "Hello.{{if .focus}} Focus on: {{.focus}}{{end}}"},
		},
	}

	msgs, err := Render(def, map[string]string{"focus": "perf"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, msgs[0]); got != "Hello. Focus on: perf" {
		t.Fatalf("with focus: %q", got)
	}

	msgs, err = Render(def, map[string]string{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, msgs[0]); got != "Hello." {
		t.Fatalf("without focus: %q", got)
	}
}

func TestRender_multiMessage(t *testing.T) {
	t.Parallel()
	def := Definition{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Help with {{.task}}."},
		},
	}
	msgs, err := Render(def, map[string]string{"task": "testing"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d", len(msgs))
	}
	if string(msgs[0].Role) != "system" {
		t.Fatalf("msgs[0].Role = %q", msgs[0].Role)
	}
	if string(msgs[1].Role) != "user" {
		t.Fatalf("msgs[1].Role = %q", msgs[1].Role)
	}
	if got := textOf(t, msgs[1]); got != "Help with testing." {
		t.Fatalf("got %q", got)
	}
}

func TestRender_includeFunc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fragment.md"), []byte("included content"), 0o644); err != nil {
		t.Fatal(err)
	}

	def := Definition{
		Messages: []Message{
			{Role: "user", Content: `Before. {{include "fragment.md"}} After.`},
		},
	}
	msgs, err := Render(def, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, msgs[0]); got != "Before. included content After." {
		t.Fatalf("got %q", got)
	}
}

func TestRender_includeFunc_notFound(t *testing.T) {
	t.Parallel()
	def := Definition{
		Messages: []Message{
			{Role: "user", Content: `{{include "missing.md"}}`},
		},
	}
	_, err := Render(def, nil, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing include")
	}
}

func TestRender_invalidTemplate(t *testing.T) {
	t.Parallel()
	def := Definition{
		Messages: []Message{
			{Role: "user", Content: "{{.unclosed"},
		},
	}
	_, err := Render(def, nil, "")
	if err == nil {
		t.Fatal("expected error for bad template")
	}
}

func TestRender_emptyAfterRender(t *testing.T) {
	t.Parallel()
	def := Definition{
		Messages: []Message{
			{Role: "user", Content: "{{if .x}}only if x{{end}}"},
		},
	}
	msgs, err := Render(def, map[string]string{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected empty messages, got %d", len(msgs))
	}
}
