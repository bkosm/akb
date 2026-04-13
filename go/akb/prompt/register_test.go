package prompt

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubRegistrar is a thread-safe fake PromptRegistrar for tests.
type stubRegistrar struct {
	mu      sync.Mutex
	prompts map[string]*mcp.Prompt
}

func newStub() *stubRegistrar {
	return &stubRegistrar{prompts: make(map[string]*mcp.Prompt)}
}

func (s *stubRegistrar) AddPrompt(p *mcp.Prompt, _ mcp.PromptHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[p.Name] = p
}

func (s *stubRegistrar) RemovePrompts(names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range names {
		delete(s.prompts, n)
	}
}

func (s *stubRegistrar) list() []*mcp.Prompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*mcp.Prompt, 0, len(s.prompts))
	for _, p := range s.prompts {
		out = append(out, p)
	}
	return out
}

func (s *stubRegistrar) get(name string) (*mcp.Prompt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[name]
	return p, ok
}

// helpers

func writeTempPrompt(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name+PromptSuffix)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// tests

func TestNewHandler_addsPromptOnCreate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := newStub()
	h := NewHandler(reg, "mykb")

	path := writeTempPrompt(t, dir, "greet", "Hello!")
	h("greet", path, false)

	prompts := reg.list()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}
	if prompts[0].Name != "mykb/greet" {
		t.Fatalf("prompt name = %q, want mykb/greet", prompts[0].Name)
	}
}

func TestNewHandler_removesPromptOnDelete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := newStub()
	h := NewHandler(reg, "mykb")

	path := writeTempPrompt(t, dir, "bye", "Bye!")
	h("bye", path, false)

	if len(reg.list()) != 1 {
		t.Fatalf("expected 1 prompt after add, got %d", len(reg.list()))
	}

	h("bye", path, true)

	if len(reg.list()) != 0 {
		t.Fatalf("expected 0 prompts after delete, got %d", len(reg.list()))
	}
}

func TestNewHandler_deleteUnknownIsNoOp(t *testing.T) {
	t.Parallel()
	reg := newStub()
	h := NewHandler(reg, "mykb")

	// deleting a name that was never added must not panic
	h("nonexistent", "/fake/path", true)

	if len(reg.list()) != 0 {
		t.Fatalf("expected 0 prompts, got %d", len(reg.list()))
	}
}

func TestNewHandler_skipsBadFile(t *testing.T) {
	t.Parallel()
	reg := newStub()
	h := NewHandler(reg, "mykb")

	h("missing", "/nonexistent/path.prompt.md", false)

	if len(reg.list()) != 0 {
		t.Fatalf("expected 0 prompts after bad file, got %d", len(reg.list()))
	}
}

func TestNewHandler_upsertUpdatesPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := newStub()
	h := NewHandler(reg, "kb")

	path := writeTempPrompt(t, dir, "hello", "---\ndescription: v1\n---\nHello")
	h("hello", path, false)

	// overwrite and call again (simulates write event)
	if err := os.WriteFile(path, []byte("---\ndescription: v2\n---\nHello v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	h("hello", path, false)

	if len(reg.list()) != 1 {
		t.Fatalf("expected 1 prompt after upsert, got %d", len(reg.list()))
	}
	p, _ := reg.get("kb/hello")
	if p == nil || p.Description != "v2" {
		t.Fatalf("description = %q, want v2", p.Description)
	}
}

func TestNewHandler_namespaceIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := newStub()

	h1 := NewHandler(reg, "kb1")
	h2 := NewHandler(reg, "kb2")

	path := writeTempPrompt(t, dir, "shared", "shared")
	h1("shared", path, false)
	h2("shared", path, false)

	if len(reg.list()) != 2 {
		t.Fatalf("expected 2 prompts (one per kb), got %d", len(reg.list()))
	}

	h1("shared", path, true)

	if len(reg.list()) != 1 {
		t.Fatalf("expected 1 prompt after kb1 delete, got %d", len(reg.list()))
	}
	if _, ok := reg.get("kb2/shared"); !ok {
		t.Fatal("kb2/shared should still be registered")
	}
	if _, ok := reg.get("kb1/shared"); ok {
		t.Fatal("kb1/shared should have been removed")
	}
}

func TestNewHandler_setsPromptArgumentsFromFrontmatter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := newStub()
	h := NewHandler(reg, "kb")

	content := "---\ndescription: Code review\narguments:\n  - name: lang\n    required: true\n---\nReview {{.lang}}"
	path := writeTempPrompt(t, dir, "review", content)
	h("review", path, false)

	p, ok := reg.get("kb/review")
	if !ok {
		t.Fatal("prompt not registered")
	}
	if p.Description != "Code review" {
		t.Fatalf("description = %q, want Code review", p.Description)
	}
	if len(p.Arguments) != 1 || p.Arguments[0].Name != "lang" || !p.Arguments[0].Required {
		t.Fatalf("arguments = %+v", p.Arguments)
	}
}
