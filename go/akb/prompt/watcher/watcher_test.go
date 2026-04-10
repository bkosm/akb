package watcher

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const testSuffix = ".prompt.md"

func waitFor(t *testing.T, events *eventCollector, count int, timeout time.Duration) []Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		got := events.all()
		if len(got) >= count {
			return got
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d events, got %d: %+v", count, len(got), got)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type eventCollector struct {
	mu     sync.Mutex
	events []Event
}

func (c *eventCollector) handle(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *eventCollector) all() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]Event, len(c.events))
	copy(cp, c.events)
	return cp
}

func TestWatch_createFile(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	col := &eventCollector{}

	w, err := Watch(dir, testSuffix, col.handle)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	path := filepath.Join(dir, "hello.prompt.md")
	if err := os.WriteFile(path, []byte("Hello!"), 0o644); err != nil {
		t.Fatal(err)
	}

	events := waitFor(t, col, 1, 5*time.Second)
	found := false
	for _, ev := range events {
		if ev.Name == "hello" && !ev.Deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected create event for hello, got %+v", events)
	}
}

func TestWatch_modifyFile(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.prompt.md")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	col := &eventCollector{}
	w, err := Watch(dir, testSuffix, col.handle)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	events := waitFor(t, col, 1, 5*time.Second)
	found := false
	for _, ev := range events {
		if ev.Name == "test" && !ev.Deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected modify event for test, got %+v", events)
	}
}

func TestWatch_deleteFile(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.prompt.md")
	if err := os.WriteFile(path, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}

	col := &eventCollector{}
	w, err := Watch(dir, testSuffix, col.handle)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	events := waitFor(t, col, 1, 5*time.Second)
	found := false
	for _, ev := range events {
		if ev.Name == "gone" && ev.Deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected delete event for gone, got %+v", events)
	}
}

func TestWatch_newSubdirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	col := &eventCollector{}

	w, err := Watch(dir, testSuffix, col.handle)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(sub, "nested.prompt.md"), []byte("Nested!"), 0o644); err != nil {
		t.Fatal(err)
	}

	events := waitFor(t, col, 1, 5*time.Second)
	found := false
	for _, ev := range events {
		if ev.Name == "sub/nested" && !ev.Deleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected create event for sub/nested, got %+v", events)
	}
}

func TestWatch_ignoresNonMatchingFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("fsnotify test")
	}
	dir := t.TempDir()
	col := &eventCollector{}

	w, err := Watch(dir, testSuffix, col.handle)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("Not a prompt"), 0o644); err != nil {
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
