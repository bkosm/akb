package localfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bkosm/akb/go/akb/config"
)

func TestLocalFS_Retrieve(t *testing.T) {
	t.Parallel()
	validBody := `{
  "kbs": {
    "kb1": {
      "rclone_remote": ":s3,env_auth=true:my-bucket/docs/",
      "mount": "/data/kb"
    }
  }
}`
	tests := []struct {
		name        string
		pathSuffix  string
		writeBody   *string
		wantErr     bool
		wantKBCount int
		check       func(t *testing.T, path string, got config.Config)
	}{
		{
			name:        "valid json",
			pathSuffix:  "config.json",
			writeBody:   strPtr(validBody),
			wantKBCount: 1,
			check: func(t *testing.T, _ string, got config.Config) {
				t.Helper()
				kbEntry, ok := got.KBs["kb1"]
				if !ok {
					t.Fatal("missing key kb1")
				}
				if kbEntry.RcloneRemote != ":s3,env_auth=true:my-bucket/docs/" {
					t.Fatalf("RcloneRemote = %q", kbEntry.RcloneRemote)
				}
				if kbEntry.Mount != "/data/kb" {
					t.Fatalf("Mount = %q", kbEntry.Mount)
				}
			},
		},
		{
			name:       "invalid json",
			pathSuffix: "bad.json",
			writeBody:  strPtr(`{not json`),
			wantErr:    true,
		},
		{
			name:        "missing file creates empty config",
			pathSuffix:  filepath.Join("nested", "config.json"),
			writeBody:   nil,
			wantKBCount: 0,
			check: func(t *testing.T, path string, got config.Config) {
				t.Helper()
				if got.KBs == nil {
					t.Fatal("expected non-nil KBs map")
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("config file not created: %v", err)
				}
				l := &LocalFS{Path: path}
				got2, err := l.Retrieve(context.Background())
				if err != nil {
					t.Fatalf("second Retrieve: %v", err)
				}
				if len(got2.KBs) != 0 {
					t.Fatalf("second len(KBs) = %d, want 0", len(got2.KBs))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tt.pathSuffix)
			if tt.writeBody != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(*tt.writeBody), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			l := &LocalFS{Path: path}
			got, err := l.Retrieve(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Retrieve: %v", err)
			}
			if len(got.KBs) != tt.wantKBCount {
				t.Fatalf("len(KBs) = %d, want %d", len(got.KBs), tt.wantKBCount)
			}
			if tt.check != nil {
				tt.check(t, path, got)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestLocalFS_Retrieve_expandsEnvInPath(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "cfg.json")
	t.Setenv("AKB_LOCALFS_TEST_PATH", realPath)
	if err := os.WriteFile(realPath, []byte(`{"kbs":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &LocalFS{Path: "$AKB_LOCALFS_TEST_PATH"}
	got, err := l.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.KBs == nil {
		t.Fatal("expected non-nil KBs map after unmarshal")
	}
}

func TestLocalFS_Save_roundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "out.json")
	want := config.Config{
		KBs: map[config.Unique]config.KB{
			"u1": {
				RcloneRemote: ":s3,env_auth=true:bucket/prefix/",
				Mount:        "/mnt/kb",
			},
		},
	}
	l := &LocalFS{Path: path}
	if err := l.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := l.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got.KBs) != len(want.KBs) {
		t.Fatalf("len(KBs) = %d, want %d", len(got.KBs), len(want.KBs))
	}
	g1, ok := got.KBs["u1"]
	if !ok {
		t.Fatal("missing u1")
	}
	w1 := want.KBs["u1"]
	if g1.RcloneRemote != w1.RcloneRemote {
		t.Fatalf("RcloneRemote = %q, want %q", g1.RcloneRemote, w1.RcloneRemote)
	}
	if g1.Mount != w1.Mount {
		t.Fatalf("Mount = %q, want %q", g1.Mount, w1.Mount)
	}
}
