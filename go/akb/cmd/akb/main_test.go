package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bkosm/akb/go/akb/config"
	configlocalfs "github.com/bkosm/akb/go/akb/config/adapter/localfs"
)

func TestRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "akb")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.json")

	kbDir := filepath.Join(home, "test-kb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}

	localKBConfig := config.Config{
		KBs: map[config.Unique]config.KB{
			"test": {Mount: kbDir},
		},
	}
	localKBJSON, _ := json.Marshal(localKBConfig)

	tests := []struct {
		name      string
		writeBody string
		ctx       func() (context.Context, context.CancelFunc)
		wantErr   func(error) bool
	}{
		{
			name:      "cancelled context with local kb",
			writeBody: string(localKBJSON),
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			wantErr: func(err error) bool { return errors.Is(err, context.Canceled) },
		},
		{
			name:      "invalid config json",
			writeBody: `{not json`,
			ctx: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			wantErr: func(err error) bool { return err != nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(cfgPath, []byte(tt.writeBody), 0o644); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := tt.ctx()
			defer cancel()
			configurer := &configlocalfs.LocalFS{Path: cfgPath}
			_, srvT := mcp.NewInMemoryTransports()
			err := run(ctx, configurer, configurer.BackendInfo(), srvT)
			if !tt.wantErr(err) {
				t.Fatalf("run: err = %v", err)
			}
		})
	}
}
