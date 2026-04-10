package endpoints

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RegisterFunc func(context.Context, *mcp.Server) error
