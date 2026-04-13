package filewatch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const testSuffix = ".prompt.md"

// collector accumulates OnFile calls in a thread-safe slice.
type collector struct {
	mu    sync.Mutex
	calls []call
}

type call struct {
	name    string
	path    string
	deleted bool
}

func (c *collector) onFile(name, path string, deleted bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call{name, path, deleted})
}

func (c *collector) all() []call {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]call, len(c.calls))
	copy(cp, c.calls)
	return cp
}

func waitFor(t *testing.T, col *collector, count int, timeout time.Duration) []call {
	t.Helper()
	deadline := time.After(timeout)
	for {
		got := col.all()
		if len(got) >= count {
			return got
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d calls, got %d: %+v", count, len(got), got)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// --- initial walk ---

func TestRegister_walksExistingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.prompt.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.prompt.md"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	// non-matching file — must not appear
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	col := &collector{}
	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	calls := col.all()
	if len(calls) != 2 {
		t.Fatalf("expected 2 initial calls, got %d: %+v", len(calls), calls)
	}
	names := map[string]bool{}
	for _, c := range calls {
		if c.deleted {
			t.Fatalf("initial walk should not produce deleted=true, got %+v", c)
		}
		names[c.name] = true
	}
	if !names["a"] || !names["b"] {
		t.Fatalf("expected names a and b, got %+v", names)
	}
}

func TestRegister_walksSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.prompt.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	col := &collector{}
	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	calls := col.all()
	if len(calls) != 1 || calls[0].name != "sub/nested" {
		t.Fatalf("expected sub/nested, got %+v", calls)
	}
}

func TestRegister_emptyDir(t *testing.T) {
	dir := t.TempDir()
	col := &collector{}
	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if got := col.all(); len(got) != 0 {
		t.Fatalf("expected no initial calls for empty dir, got %+v", got)
	}
}

// --- fsnotify events (skipped in short mode) ---

func TestRegister_createFile(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	col := &collector{}

	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	path := filepath.Join(dir, "new.prompt.md")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := waitFor(t, col, 1, 5*time.Second)
	found := false
	for _, c := range calls {
		if c.name == "new" && !c.deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected create event for new, got %+v", calls)
	}
}

func TestRegister_modifyFile(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.prompt.md")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	col := &collector{}
	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// wait for initial walk to flush, then modify
	time.Sleep(100 * time.Millisecond)
	initialLen := len(col.all())

	if err := os.WriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := waitFor(t, col, initialLen+1, 5*time.Second)
	found := false
	for _, c := range calls[initialLen:] {
		if c.name == "edit" && !c.deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected modify event for edit, got %+v", calls)
	}
}

func TestRegister_deleteFile(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bye.prompt.md")
	if err := os.WriteFile(path, []byte("gone"), 0o644); err != nil {
		t.Fatal(err)
	}

	col := &collector{}
	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	time.Sleep(100 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	calls := waitFor(t, col, 2, 5*time.Second) // 1 initial + 1 delete
	found := false
	for _, c := range calls {
		if c.name == "bye" && c.deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected delete event for bye, got %+v", calls)
	}
}

func TestRegister_ignoresNonMatchingSuffix(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	col := &collector{}

	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	if got := col.all(); len(got) != 0 {
		t.Fatalf("expected no events for non-matching files, got %+v", got)
	}
}

func TestRegister_newSubdirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	col := &collector{}

	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(sub, "deep.prompt.md"), []byte("deep"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := waitFor(t, col, 1, 5*time.Second)
	found := false
	for _, c := range calls {
		if c.name == "sub/deep" && !c.deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected create event for sub/deep, got %+v", calls)
	}
}

func TestRegister_stopReleasesResources(t *testing.T) {
	dir := t.TempDir()
	col := &collector{}

	stop, err := Register(dir, testSuffix, col.onFile)
	if err != nil {
		t.Fatal(err)
	}
	stop() // must not panic or block
}
