package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Render executes Go templates in each message and returns MCP PromptMessages.
// baseDir is used to resolve include paths; pass "" to disable include.
func Render(def Definition, args map[string]string, baseDir string) ([]*mcp.PromptMessage, error) {
	data := make(map[string]any, len(args))
	for k, v := range args {
		data[k] = v
	}

	funcMap := template.FuncMap{}
	if baseDir != "" {
		funcMap["include"] = makeIncludeFunc(baseDir)
	}

	out := make([]*mcp.PromptMessage, 0, len(def.Messages))
	for i, msg := range def.Messages {
		tmpl, err := template.New(fmt.Sprintf("msg-%d", i)).
			Funcs(funcMap).
			Option("missingkey=zero").
			Parse(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("parse template for message %d (%s): %w", i, msg.Role, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("render message %d (%s): %w", i, msg.Role, err)
		}

		text := strings.TrimSpace(buf.String())
		if text == "" {
			continue
		}

		out = append(out, &mcp.PromptMessage{
			Role:    mcp.Role(msg.Role),
			Content: &mcp.TextContent{Text: text},
		})
	}
	return out, nil
}

func makeIncludeFunc(baseDir string) func(string) (string, error) {
	return func(relPath string) (string, error) {
		absPath := filepath.Join(baseDir, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("include %q: %w", relPath, err)
		}
		return string(data), nil
	}
}
