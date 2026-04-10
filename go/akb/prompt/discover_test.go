package prompt

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestDiscover_findsPromptFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	files := map[string]string{
		"simple.prompt.md":     "Hello.",
		"sub/nested.prompt.md": "Nested.",
		"not-a-prompt.md":      "Ignored.",
		"deep/a/b/c.prompt.md": "Deep.",
		"readme.txt":           "Also ignored.",
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(content), 0o644)
	}

	defs, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })

	if len(defs) != 3 {
		names := make([]string, len(defs))
		for i, d := range defs {
			names[i] = d.Name
		}
		t.Fatalf("len = %d, want 3; names = %v", len(defs), names)
	}

	want := []string{"deep/a/b/c", "simple", "sub/nested"}
	for i, d := range defs {
		if d.Name != want[i] {
			t.Fatalf("defs[%d].Name = %q, want %q", i, d.Name, want[i])
		}
		if d.SourcePath == "" {
			t.Fatalf("defs[%d].SourcePath is empty", i)
		}
	}
}

func TestDiscover_emptyDir(t *testing.T) {
	t.Parallel()
	defs, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Fatalf("len = %d, want 0", len(defs))
	}
}

func TestDiscover_skipsUnparseableFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Valid file
	os.WriteFile(filepath.Join(dir, "good.prompt.md"), []byte("Hello."), 0o644)

	// File with invalid frontmatter
	os.WriteFile(filepath.Join(dir, "bad.prompt.md"), []byte("---\n: [invalid yaml\n---\nBody."), 0o644)

	defs, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("len = %d, want 1 (should skip bad file)", len(defs))
	}
	if defs[0].Name != "good" {
		t.Fatalf("Name = %q", defs[0].Name)
	}
}
