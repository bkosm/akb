//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startServer launches the AKB server as a subprocess and returns an MCP
// client session connected to it. The caller must call cleanup() when done.
func startServer(t *testing.T, configPath string) (*mcp.ClientSession, func()) {
	t.Helper()

	cmd := exec.Command("go", "run", ".", "local", "--path", configPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}

	transport := &mcp.IOTransport{
		Reader: io.NopCloser(stdout),
		Writer: stdin,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		cancel()
		_ = cmd.Process.Kill()
		t.Fatalf("connect: %v", err)
	}

	cleanup := func() {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return session, cleanup
}

func TestIntegration_ListKBs_Empty(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	session, cleanup := startServer(t, configPath)
	defer cleanup()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_kbs", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("list_kbs: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_kbs error: %v", result.Content)
	}

	text := extractText(t, result)
	var out struct {
		KBs []any `json:"kbs"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal list_kbs output: %v\nraw: %s", err, text)
	}
	if len(out.KBs) != 0 {
		t.Fatalf("expected 0 KBs, got %d", len(out.KBs))
	}
}

func TestIntegration_NewKB_Local(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	kbDir := t.TempDir()

	session, cleanup := startServer(t, configPath)
	defer cleanup()

	ctx := context.Background()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "new_kb",
		Arguments: map[string]any{
			"name":  "test-kb",
			"mount": kbDir,
		},
	})
	if err != nil {
		t.Fatalf("new_kb: %v", err)
	}
	if result.IsError {
		t.Fatalf("new_kb error: %v", result.Content)
	}

	listResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_kbs", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("list_kbs after new_kb: %v", err)
	}
	text := extractText(t, listResult)
	var out struct {
		KBs []struct {
			Name        string `json:"name"`
			MountStatus string `json:"mount_status"`
		} `json:"kbs"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, text)
	}
	if len(out.KBs) != 1 {
		t.Fatalf("expected 1 KB, got %d", len(out.KBs))
	}
	if out.KBs[0].Name != "test-kb" {
		t.Fatalf("name = %q, want test-kb", out.KBs[0].Name)
	}
	if out.KBs[0].MountStatus != "mounted" {
		t.Fatalf("mount_status = %q, want \"mounted\"", out.KBs[0].MountStatus)
	}
}

func TestIntegration_UseKB_LocalNoop(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	kbDir := t.TempDir()

	session, cleanup := startServer(t, configPath)
	defer cleanup()

	ctx := context.Background()

	_, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "new_kb",
		Arguments: map[string]any{
			"name":  "test-kb",
			"mount": kbDir,
		},
	})
	if err != nil {
		t.Fatalf("new_kb: %v", err)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "use_kb",
		Arguments: map[string]any{
			"name":   "test-kb",
			"action": "mount",
		},
	})
	if err != nil {
		t.Fatalf("use_kb: %v", err)
	}
	if result.IsError {
		t.Fatalf("use_kb error: %v", result.Content)
	}

	text := extractText(t, result)
	if !strings.Contains(text, "no mount action needed") {
		t.Fatalf("expected noop message, got: %s", text)
	}
}

// extractText returns the text content from the first TextContent in a CallToolResult.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatalf("no text content in result: %+v", result.Content)
	return ""
}
