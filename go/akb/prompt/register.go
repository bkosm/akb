package prompt

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/bkosm/akb/prompt/watcher"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterForKB discovers .prompt.md files in mountPath, registers them as
// MCP prompts namespaced under kbName, and starts an fsnotify watcher.
// Returns a stop function to tear down the watcher.
func RegisterForKB(server *mcp.Server, kbName, mountPath string) (stop func(), err error) {
	mountPath = filepath.Clean(os.ExpandEnv(mountPath))

	mu := &sync.Mutex{}
	registered := make(map[string]struct{})

	register := func(kbName string, def Definition) {
		name := kbName + "/" + def.Name
		mcpPrompt := &mcp.Prompt{
			Name:        name,
			Title:       name,
			Description: def.Description,
		}
		for _, arg := range def.Arguments {
			mcpPrompt.Arguments = append(mcpPrompt.Arguments, &mcp.PromptArgument{
				Name:        arg.Name,
				Description: arg.Description,
				Required:    arg.Required,
			})
		}

		handler := makeHandler(def)
		server.AddPrompt(mcpPrompt, handler)

		mu.Lock()
		registered[name] = struct{}{}
		mu.Unlock()
	}

	defs, err := Discover(mountPath)
	if err != nil {
		return nil, fmt.Errorf("discover prompts in %q: %w", mountPath, err)
	}
	for _, def := range defs {
		register(kbName, def)
	}

	w, err := watcher.Watch(mountPath, PromptSuffix, func(ev watcher.Event) {
		name := kbName + "/" + ev.Name
		if ev.Deleted {
			mu.Lock()
			_, was := registered[name]
			delete(registered, name)
			mu.Unlock()
			if was {
				server.RemovePrompts(name)
			}
			return
		}

		def, parseErr := ParseFile(ev.Path)
		if parseErr != nil {
			slog.Warn("prompt watcher: skip file", "path", ev.Path, "err", parseErr)
			return
		}
		def.Name = ev.Name
		def.SourcePath = ev.Path
		register(kbName, def)
	})
	if err != nil {
		slog.Warn("prompt watcher failed, prompts won't auto-update", "kb", kbName, "err", err)
		return func() {}, nil
	}

	return w.Stop, nil
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
