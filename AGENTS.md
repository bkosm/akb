# AGENTS.md

This file provides guidance to coding agents when working with code in this repository.

## Commands

All Go code lives under `go/akb/`. Run commands from the repo root.

```bash
# Build binary
make build

# Run all tests
make test

# Run tests for a specific package
go test ./go/akb/endpoints/newkb/
go test -run TestFunctionName ./go/akb/...

# Lint
make lint

# Format and vet
make fmt
make vet

# Run the server (local config backend)
./bin/stdio.sh local [--path ~/.config/akb/config.json]

# Run the server (S3 config backend)
./bin/stdio.sh s3 [--bucket akb] [--region us-east-1] [--config-key config.json]
```

`bin/stdio.sh` loads `.envrc` via direnv (if present) and runs `go run go/akb/cmd/stdio/main.go "$@"`.

## Using knowledge bases

Agents interact with KBs using standard file tools on local mount paths.

1. Call `list_kbs` to discover available KBs and their mount paths
2. Use standard file tools on the returned paths:
   - Read files: Read tool
   - Write files: Write tool
   - Find files: Glob tool
   - Search content: Grep tool
3. Call `new_kb` to create a new knowledge base

KBs can be backed by remote storage (mounted via rclone FUSE) or plain local directories. The agent doesn't need to know the difference — it just reads and writes files at the mount path.

## Prompts

Prompts are auto-discovered from `*.prompt.md` files in KBs. Write a `.prompt.md` file to any KB and it becomes an MCP slash-command prompt automatically.

### Prompt file format

YAML frontmatter (optional) + markdown body with Go `text/template` syntax:

```markdown
---
description: Review code for best practices
arguments:
  - name: language
    required: true
    description: The programming language
---

Review the following {{.language}} code for quality issues.
```

- **Single message**: body with no role headers = single `user` message
- **Multi-message**: use `@role` headers at any heading level (`# @system`, `## @user`, etc.)
- **Template functions**: Go `text/template` builtins + `include "relative/path"` for composing from fragments
- **Naming**: prompt name = `<kb-name>/<relative-path-without-.prompt.md-suffix>`

### File watcher

Prompts are discovered on KB mount and watched via fsnotify. Creating, editing, or deleting a `.prompt.md` file updates the MCP prompt list in real time.

## Architecture

This is an MCP (Model Context Protocol) server that manages knowledge bases as mounted directories. It communicates over stdio using `github.com/modelcontextprotocol/go-sdk`.

### Layered structure

```
cmd/stdio/main.go            — entry point, wires config + mount manager, starts MCP server
endpoints/{name}/            — MCP tool/prompt registrations (one package per tool)
mount/                       — rclone FUSE mount lifecycle manager
prompt/                      — .prompt.md parser, template renderer, file discovery
prompt/watcher/              — fsnotify-based recursive file watcher for prompt files
config/port.go               — config.Interface (Retrieve, Save) + Config struct
config/adapter/{localfs,s3}/ — config.Interface implementations
```

### Dependency injection via context

Config and the mount manager are threaded through `context.Context`:

- `config.IntoContext` / `config.FromContext` — stores the `config.Interface` implementation
- `mount.IntoContext` / `mount.FromContext` — stores the `*mount.Manager`

`main.go` loads config first, then creates the MCP server, registers endpoints, and starts a background goroutine that mounts KBs and discovers prompts as each mount completes.

### KB storage model

Each KB in config has these fields:
- `rclone_remote` (optional) — rclone remote path spec (e.g. `:s3,env_auth=true:bucket/prefix/`)
- `mount` — local directory path (FUSE/NFS mountpoint when remote, or plain local dir)
- `mount_method` (optional) — `"fuse"`, `"nfs"`, or empty for auto (prefer FUSE, fall back to NFS)
- `rclone_args` (optional) — `map[string]string` flag overrides merged on top of defaults
- `description` (optional) — human-readable description

When `rclone_remote` is set, the mount manager starts `rclone mount` or `rclone nfsmount` depending on the resolved mount method. When empty, `mount` is used as a plain local directory. See [README.md](README.md) for rclone installation caveats and mount method details.

### Endpoints

Each package under `endpoints/` exports a single `Register(ctx context.Context, server *mcp.Server) error` function (`endpoints.RegisterFunc`). Endpoints pull the config or mount manager out of context at call time (not at registration time), so context values must be set before `Register` is called.

### Conflict handling

`config.Interface.Save` returns `config.ErrConflict` when an optimistic-lock conflict is detected. Callers must re-`Retrieve` and retry.

### Async startup mounts

KBs are mounted in a background goroutine that runs concurrently with `server.Run`. MCP tools are immediately available after startup, but remote KBs may not be mounted yet when the first request arrives. Mount failures are logged to stderr; they do not prevent the server from starting or serving local KBs.

### Config backend concurrency

- **S3 adapter**: uses `If-Match` / ETag for optimistic locking; concurrent writes detect conflicts and return `ErrConflict`.
- **localfs adapter**: no file locking — concurrent writes from multiple processes are last-writer-wins. Intended for single-process use only.

### Prompt `include` security

`{{include "path"}}` resolves paths relative to the prompt file's directory and is **not sandboxed** — a `../` traversal can read any file accessible to the server process. KB prompts are treated as trusted content.
