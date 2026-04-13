package endpoints

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterFunc is the signature for endpoint registration functions.
// Each package under endpoints/ exports a Register variable of this type.
// The function receives the context (with config and mount manager) and the
// MCP server to register tools or prompts against.
type RegisterFunc func(context.Context, *mcp.Server) error

// BoolFalse and BoolTrue are shared bool pointers used by endpoint tool
// annotations (OpenWorldHint, DestructiveHint). Shared here to avoid
// per-package duplication.
var (
	BoolFalse = false
	BoolTrue  = true
)
