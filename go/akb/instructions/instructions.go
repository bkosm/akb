// Package instructions provides embedded MCP server instructions.
package instructions

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed server.md
var serverMarkdown string

// Base returns the embedded MCP server instructions.
func Base() string {
	return strings.TrimSpace(serverMarkdown)
}

// Build returns server instructions with optional config backend identity.
func Build(backendInfo string) string {
	base := Base()
	if backendInfo == "" {
		return base
	}
	return base + fmt.Sprintf("\n\nConfig backend: %s", backendInfo)
}
