package purgekb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bkosm/akb/go/akb/config"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubConfigurer struct {
	cfg         config.Config
	saved       *config.Config
	retrieveErr error
	saveErr     error
}

func (s *stubConfigurer) Retrieve(context.Context) (config.Config, error) {
	return s.cfg, s.retrieveErr
}
func (s *stubConfigurer) Save(_ context.Context, cfg config.Config) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = &cfg
	return nil
}

func baseCtx(t *testing.T, sc *stubConfigurer) context.Context {
	t.Helper()
	return config.IntoContext(context.Background(), sc)
}

func TestHandle_RemovesKBFromConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb":    {Mount: dir},
			"other-kb": {Mount: "/other"},
		},
	}}
	ctx := baseCtx(t, sc)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Message, "my-kb") {
		t.Fatalf("message = %q, want mention of kb name", out.Message)
	}
	if sc.saved == nil {
		t.Fatal("Save not called")
	}
	if _, exists := sc.saved.KBs["my-kb"]; exists {
		t.Fatal("KB should have been removed from config")
	}
	if _, exists := sc.saved.KBs["other-kb"]; !exists {
		t.Fatal("other KB should be preserved")
	}
}

func TestHandle_KBNotFound(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{},
	}}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "nope"})
	if err == nil {
		t.Fatal("expected error for missing KB")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %q, want mention of 'not found'", err)
	}
}

func TestHandle_EmptyName(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{KBs: map[config.Unique]config.KB{}}}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestHandle_DeleteFiles_RemovesMount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: dir},
		},
	}}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb", DeleteFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	// Root directory is preserved; only its contents are removed.
	entries, statErr := os.ReadDir(dir)
	if statErr != nil {
		t.Fatalf("mount root should still exist: %v", statErr)
	}
	if len(entries) != 0 {
		t.Fatalf("mount root should be empty, got %d entries", len(entries))
	}
}

// TestHandle_DeleteFiles_RemovesContents verifies that delete_files=true
// removes nested files and subdirectories but leaves the root dir intact.
func TestHandle_DeleteFiles_RemovesContents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(subdir, "nested.txt")
	if err := os.WriteFile(nested, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: dir},
		},
	}}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb", DeleteFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	// Nested file and subdirectory must be gone.
	if _, statErr := os.Stat(nested); !os.IsNotExist(statErr) {
		t.Fatal("nested file should have been deleted")
	}
	if _, statErr := os.Stat(subdir); !os.IsNotExist(statErr) {
		t.Fatal("subdirectory should have been deleted")
	}
	// Root dir must still exist.
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		t.Fatal("root mount directory should be preserved")
	}
}

func TestHandle_DeleteFiles_False_PreservesMount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: dir},
		},
	}}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb", DeleteFiles: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(file); os.IsNotExist(statErr) {
		t.Fatal("files should be preserved when delete_files=false")
	}
}

func TestHandle_DeleteFiles_NonExistentPath(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: "/nonexistent/path/that/does/not/exist"},
		},
	}}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb", DeleteFiles: true})
	if err != nil {
		t.Fatalf("expected no error for non-existent mount path, got: %v", err)
	}
}

// TestHandle_DeleteFiles_DeregistersManager verifies that when delete_files=true
// the KB is deregistered from the mount manager before files are removed.
// After the call, the same mount path can be re-registered without conflict.
func TestHandle_DeleteFiles_DeregistersManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mgr := mount.NewManager()
	if err := mgr.Add(context.Background(), "my-kb", "", dir, mount.MethodAuto, nil); err != nil {
		t.Fatal(err)
	}

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: dir},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb", DeleteFiles: true})
	if err != nil {
		t.Fatal(err)
	}

	// After deregister + RemoveAll, re-registering the same path must succeed.
	dir2 := t.TempDir()
	if err := mgr.Add(context.Background(), "my-kb", "", dir2, mount.MethodAuto, nil); err != nil {
		t.Fatalf("re-Add after purge should succeed: %v", err)
	}
}

// TestHandle_DeleteFiles_False_UnmountsManager verifies that even with
// delete_files=false the mount manager entry is cleaned up, so a subsequent
// Add for the same path succeeds without a "already registered" conflict.
func TestHandle_DeleteFiles_False_UnmountsManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := mount.NewManager()
	if err := mgr.Add(context.Background(), "my-kb", "", dir, mount.MethodAuto, nil); err != nil {
		t.Fatal(err)
	}

	sc := &stubConfigurer{cfg: config.Config{
		KBs: map[config.Unique]config.KB{
			"my-kb": {Mount: dir},
		},
	}}
	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb", DeleteFiles: false})
	if err != nil {
		t.Fatal(err)
	}

	// Files must be preserved.
	if _, statErr := os.Stat(file); os.IsNotExist(statErr) {
		t.Fatal("files should be preserved when delete_files=false")
	}

	// Mountpoint must be deregistered — re-Add to the same path must succeed.
	if err := mgr.Add(context.Background(), "my-kb", "", dir, mount.MethodAuto, nil); err != nil {
		t.Fatalf("re-Add after purge(delete_files=false) should succeed: %v", err)
	}
}

func TestHandle_RetrieveError(t *testing.T) {
	t.Parallel()
	sc := &stubConfigurer{retrieveErr: errors.New("storage unavailable")}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "any-kb"})
	if err == nil {
		t.Fatal("expected error when Retrieve fails")
	}
	if !strings.Contains(err.Error(), "retrieve config") {
		t.Fatalf("error = %q, want mention of 'retrieve config'", err)
	}
}

func TestHandle_SaveError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sc := &stubConfigurer{
		cfg: config.Config{
			KBs: map[config.Unique]config.KB{
				"my-kb": {Mount: dir},
			},
		},
		saveErr: errors.New("conflict"),
	}
	ctx := baseCtx(t, sc)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{Name: "my-kb"})
	if err == nil {
		t.Fatal("expected error when Save fails")
	}
	if !strings.Contains(err.Error(), "save config") {
		t.Fatalf("error = %q, want mention of 'save config'", err)
	}
}

func TestHandle_NoConfigInContext(t *testing.T) {
	t.Parallel()
	_, _, err := Handle(context.Background(), &mcp.CallToolRequest{}, Input{Name: "my-kb"})
	if err == nil {
		t.Fatal("expected error when config not in context")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("error = %q, want mention of 'config'", err)
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}
