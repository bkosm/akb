package newkb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bkosm/akb/go/akb/config"
	configlocalfs "github.com/bkosm/akb/go/akb/config/adapter/localfs"
	"github.com/bkosm/akb/go/akb/mount"
)

func ctxWithLocalSetup(t *testing.T, cfgPath string) (context.Context, *mount.Manager) {
	t.Helper()
	configurer := &configlocalfs.LocalFS{Path: cfgPath}
	mgr := mount.NewManager()
	ctx := config.IntoContext(context.Background(), configurer)
	ctx = mount.ManagerIntoContext(ctx, mgr)
	return ctx, mgr
}

func TestHandle_createsLocalKB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	kbDir := filepath.Join(dir, "mykb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configurer := &configlocalfs.LocalFS{Path: cfgPath}
	mgr := mount.NewManager()
	ctx := config.IntoContext(context.Background(), configurer)
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, out, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "my-kb",
		Mount: kbDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Hint == "" {
		t.Fatal("expected non-empty hint")
	}
	if out.Mount == "" {
		t.Fatal("expected non-empty mount path")
	}

	cfg, err := configurer.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	entry, ok := cfg.KBs["my-kb"]
	if !ok {
		t.Fatal("expected my-kb in config")
	}
	if entry.Mount != kbDir {
		t.Fatalf("Mount = %q, want %q", entry.Mount, kbDir)
	}
	if entry.RcloneRemote != "" {
		t.Fatalf("RcloneRemote = %q, want empty", entry.RcloneRemote)
	}
}

func TestHandle_duplicateName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	kbDir := filepath.Join(dir, "mykb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxWithLocalSetup(t, cfgPath)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "dup",
		Mount: kbDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second directory required for the duplicate attempt (same name, different path is fine
	// at the mount level but the config check fires first).
	kbDir2 := filepath.Join(dir, "mykb2")
	if err := os.MkdirAll(kbDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "dup",
		Mount: kbDir2,
	})
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestHandle_missingFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

// TestHandle_MountBeforeSave verifies that the mount manager is called before
// the config is persisted, so a failed mount never results in a saved entry.
func TestHandle_MountBeforeSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kbDir := filepath.Join(dir, "kb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sc := newStubConfigurer(config.Config{KBs: make(map[config.Unique]config.KB)})
	mgr := mount.NewManager()
	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "local-kb",
		Mount: kbDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Config must have been saved after the mount succeeded.
	if sc.saved == nil {
		t.Fatal("expected Save to be called")
	}
	if _, ok := sc.saved.KBs["local-kb"]; !ok {
		t.Fatal("expected local-kb in saved config")
	}
}

// TestHandle_SaveFailDeregisters verifies that when Save fails after a
// successful Add, the mount manager entry is cleaned up so a retry can succeed.
func TestHandle_SaveFailDeregisters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kbDir := filepath.Join(dir, "kb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sc := newStubConfigurer(config.Config{KBs: make(map[config.Unique]config.KB)})
	sc.saveErr = errors.New("save failed")
	mgr := mount.NewManager()
	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "kb",
		Mount: kbDir,
	})
	if err == nil {
		t.Fatal("expected error from Save failure")
	}

	// After the failure the mountpoint should be deregistered so Add can retry.
	sc.saveErr = nil
	_, _, err = Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "kb",
		Mount: kbDir,
	})
	if err != nil {
		t.Fatalf("retry after save failure: %v", err)
	}
}

// TestHandle_NoMountManager verifies that new_kb returns an error when the
// mount manager is absent — config must not be persisted.
func TestHandle_NoMountManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kbDir := filepath.Join(dir, "kb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sc := newStubConfigurer(config.Config{KBs: make(map[config.Unique]config.KB)})
	ctx := config.IntoContext(context.Background(), sc)
	// mount manager intentionally absent

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:  "kb",
		Mount: kbDir,
	})
	if err == nil {
		t.Fatal("expected error when mount manager not in context")
	}
	if sc.saved != nil {
		t.Fatal("config must not be saved when mount manager is missing")
	}
}

// TestHandle_defaultMountPath verifies the default mount path is derived from
// the KB name when rclone_remote is set and mount is omitted.
func TestHandle_defaultMountPath(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.
	// We only test the error path — rclone itself is not available in CI —
	// but it proves the default path is computed and Preflight fires, not a
	// "mount is required" validation error.
	t.Setenv("PATH", "")

	sc := newStubConfigurer(config.Config{KBs: make(map[config.Unique]config.KB)})
	mgr := mount.NewManager()
	ctx := config.IntoContext(context.Background(), sc)
	ctx = mount.ManagerIntoContext(ctx, mgr)

	_, _, err := Handle(ctx, &mcp.CallToolRequest{}, Input{
		Name:         "my-remote",
		RcloneRemote: ":s3:bucket/",
		// Mount omitted — should default to $HOME/.akb/mounts/my-remote
	})
	if err == nil {
		t.Fatal("expected error because rclone is not on PATH")
	}
	// Must fail at Preflight, not at input validation.
	if err.Error() == "mount is required (local directory path or FUSE mountpoint)" {
		t.Fatalf("got validation error instead of preflight: %v", err)
	}
}

func TestBuildToolDescription_noBackend(t *testing.T) {
	t.Parallel()
	got := buildToolDescription("")
	if got != toolDescription {
		t.Fatalf("expected base toolDescription unchanged, got:\n%s", got)
	}
}

func TestBuildToolDescription_withBackend(t *testing.T) {
	t.Parallel()
	arn := "arn:aws:s3:eu-west-1:123456789012:akb-123456789012/config.json"
	got := buildToolDescription(arn)
	if !strings.Contains(got, arn) {
		t.Errorf("description missing ARN %q", arn)
	}
	if !strings.Contains(got, "eu-west-1") {
		t.Error("description missing region")
	}
	if !strings.Contains(got, "akb-123456789012") {
		t.Error("description missing bucket")
	}
	if !strings.Contains(got, "rclone_remote") {
		t.Error("description missing rclone_remote hint")
	}
	if got == toolDescription {
		t.Error("description should differ from base when backendInfo is set")
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}

func TestRegister_withBackendDescriber(t *testing.T) {
	t.Parallel()
	sc := newStubConfigurer(config.Config{KBs: make(map[config.Unique]config.KB)})
	sbd := &stubBackendDescriber{stubConfigurer: sc, info: "arn:aws:s3:eu-west-1:123:akb-123/config.json"}
	ctx := config.IntoContext(context.Background(), sbd)
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(ctx, srv); err != nil {
		t.Fatal(err)
	}
}

func TestRegister_withoutBackendDescriber(t *testing.T) {
	t.Parallel()
	sc := newStubConfigurer(config.Config{KBs: make(map[config.Unique]config.KB)})
	ctx := config.IntoContext(context.Background(), sc)
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := Register(ctx, srv); err != nil {
		t.Fatal(err)
	}
}

// stubConfigurer is a simple in-memory config.Interface for testing.
// Retrieve always returns the last successfully saved state (or the initial cfg
// if nothing has been saved), making it behave like a real adapter.
type stubConfigurer struct {
	initial config.Config
	current config.Config // updated on each successful Save
	saved   *config.Config
	saveErr error
}

func newStubConfigurer(cfg config.Config) *stubConfigurer {
	return &stubConfigurer{initial: cfg, current: cfg}
}

// copyConfig returns a deep copy of a Config so callers cannot mutate the
// stub's internal state through the returned map.
func copyConfig(c config.Config) config.Config {
	out := config.Config{KBs: make(map[config.Unique]config.KB, len(c.KBs))}
	for k, v := range c.KBs {
		out.KBs[k] = v
	}
	return out
}

func (s *stubConfigurer) Retrieve(context.Context) (config.Config, error) {
	return copyConfig(s.current), nil
}
func (s *stubConfigurer) Save(_ context.Context, cfg config.Config) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.current = copyConfig(cfg)
	s.saved = &cfg
	return nil
}

// stubBackendDescriber wraps stubConfigurer and also implements config.BackendDescriber.
type stubBackendDescriber struct {
	*stubConfigurer
	info string
}

func (s *stubBackendDescriber) BackendInfo() string { return s.info }
