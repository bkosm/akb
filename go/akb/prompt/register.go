package prompt

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/bkosm/akb/go/akb/filewatch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PromptRegistrar is satisfied by *mcp.Server and allows tests to use stubs.
type PromptRegistrar interface {
	AddPrompt(p *mcp.Prompt, h mcp.PromptHandler)
	RemovePrompts(names ...string)
}

// NewHandler returns a filewatch.OnFile callback that parses .prompt.md files
// and registers or removes them as MCP prompts on server, namespaced under
// kbName.
func NewHandler(server PromptRegistrar, kbName string) filewatch.OnFile {
	mu := &sync.Mutex{}
	registered := make(map[string]struct{})

	register := func(name string, def Definition) {
		fullName := kbName + "/" + name
		mcpPrompt := &mcp.Prompt{
			Name:        fullName,
			Title:       fullName,
			Description: def.Description,
		}
		for _, arg := range def.Arguments {
			mcpPrompt.Arguments = append(mcpPrompt.Arguments, &mcp.PromptArgument{
				Name:        arg.Name,
				Description: arg.Description,
				Required:    arg.Required,
			})
		}
		server.AddPrompt(mcpPrompt, makeHandler(def))

		mu.Lock()
		registered[fullName] = struct{}{}
		mu.Unlock()
	}

	return func(name, path string, deleted bool) {
		fullName := kbName + "/" + name
		if deleted {
			mu.Lock()
			_, was := registered[fullName]
			delete(registered, fullName)
			mu.Unlock()
			if was {
				server.RemovePrompts(fullName)
			}
			return
		}

		def, err := ParseFile(path)
		if err != nil {
			slog.Warn("prompt handler: skip file", "path", path, "err", err)
			return
		}
		def.Name = name
		def.SourcePath = path
		register(name, def)
	}
}

func makeHandler(def Definition) mcp.PromptHandler {
	sourcePath := def.SourcePath
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		current, err := ParseFile(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("read prompt %q: %w", sourcePath, err)
		}

		baseDir := filepath.Dir(sourcePath)
		messages, err := Render(current, req.Params.Arguments, baseDir)
		if err != nil {
			return nil, fmt.Errorf("render prompt: %w", err)
		}

		return &mcp.GetPromptResult{
			Description: current.Description,
			Messages:    messages,
		}, nil
	}
}
