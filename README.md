# AKB — Agentic Knowledge Base

[![CI](https://github.com/bkosm/akb/actions/workflows/ci.yml/badge.svg)](https://github.com/bkosm/akb/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/badge/go-1.25%2B-blue)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/license-Apache%202.0-green)](LICENSE)

An MCP server that manages knowledge bases as mounted directories. AI agents interact with KBs using standard file tools (Read, Write, Glob, Grep) on local paths — remote storage is transparently mounted via [rclone](https://rclone.org/).

## Installation

### go install

```bash
go install github.com/bkosm/akb/go/akb/cmd/akb@latest
```

### Download binary

Pre-built binaries for macOS and Linux (amd64/arm64) are available on the [Releases](https://github.com/bkosm/akb/releases) page.

### From source

```bash
git clone https://github.com/bkosm/akb.git
cd akb
make build
# binary at bin/akb
```

## Configure as MCP

### Local config

- <a href="https://cursor.com/en-US/install-mcp?name=akb-s3&config=eyJlbnYiOnsiQVdTX1BST0ZJTEUiOiJzc28tcHJvZmlsZSIsIkFXU19SRUdJT04iOiJidWNrZXQtcmVnaW9uIn0sImNvbW1hbmQiOiJha2IgczMifQ%3D%3D" target="_blank"><img src="https://cursor.com/deeplink/mcp-install-dark.svg" alt="Add to Cursor"></a>
- `claude mcp add akb-local -- akb local`

### S3 config

- <a href="https://cursor.com/en-US/install-mcp?name=akb-local&config=eyJjb21tYW5kIjoiYWtiIGxvY2FsIn0%3D" target="_blank"><img src="https://cursor.com/deeplink/mcp-install-dark.svg" alt="Add to Cursor"></a>
- `claude mcp add akb-s3 -e AWS_PROFILE=profile -e AWS_REGION=eu-west-1 -- akb s3`


## Architecture

```mermaid
flowchart TD
    Agent["AI Agent\n(Cursor / Claude / etc.)"]
    MCP["AKB MCP Server\nstdio transport"]
    Config["Config backend\nlocal JSON or S3"]
    Mount["Mount manager\nrclone FUSE/NFS"]
    KB1["Local KB\nplain directory"]
    KB2["Remote KB\nS3 / GCS / SFTP"]
    Prompts["Prompt watcher\n*.prompt.md"]

    Agent -->|"MCP tools"| MCP
    MCP --> Config
    MCP --> Mount
    Mount --> KB1
    Mount --> KB2
    MCP --> Prompts
    Prompts -->|"MCP prompts"| Agent
```

## Prerequisites

- **Go 1.25+**
- **rclone** (only needed for remote-backed KBs) — see [docs/rclone-setup.md](docs/rclone-setup.md) or install in one line:
  ```bash
  curl -fsSL https://raw.githubusercontent.com/bkosm/akb/main/bin/install-rclone.sh | bash
  ```

## KB configuration

Each KB entry in the config has these fields:

```json
{
  "rclone_remote": ":s3,provider=AWS,env_auth=true,region=eu-west-1:my-bucket/prefix/",
  "mount": "$HOME/my-repo/.akb/my-kb",
  "mount_method": "nfs",
  "rclone_args": {
    "vfs-cache-max-size": "5G",
    "read-only": ""
  },
  "backup": {
    "enabled": true,
    "keep": 3
  },
  "description": "My knowledge base"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `mount` | yes | Local directory path. For project-scoped KBs, prefer `.akb/<name>` under the repository root (e.g. `$HOME/my-repo/.akb/my-kb`) and add `.akb` to the repo's `.gitignore`. For global KBs shared across projects, use `$HOME/.akb/mounts/<name>`. For remote KBs, omitting mount defaults to `$HOME/.akb/mounts/<name>`. |
| `rclone_remote` | no | rclone remote spec. Omit for a plain local directory. Format: `:backend,opt=val:bucket/path`. See [rclone docs](https://rclone.org/overview/#syntax-of-remote-paths). |
| `mount_method` | no | `"fuse"`, `"nfs"`, or omit for auto. |
| `rclone_args` | no | Flag overrides as `{"flag-name": "value"}` (without `--` prefix). Empty value for boolean flags. Merged on top of defaults. |
| `backup` | no | Backup settings. `enabled` defaults to `false`; `keep` defaults to `3` when backups are enabled. |
| `description` | no | Human-readable description. |

All KB config fields (`mount`, `rclone_remote`, `rclone_args`) support `$ENV_VAR` expansion at runtime. When using a remote config backend (S3), always use env var prefixes (e.g. `$HOME`) instead of bare absolute paths so configs stay portable across developers and machines. Local config backends can use absolute paths.

### Concurrent AKB processes

Do not run two AKB processes with the same remote KB configured to the same
local mountpoint. AKB owns only the mounts it starts in its own process. If a
remote mountpoint is already mounted but is not registered in the current AKB
process, AKB refuses to take it over and reports the KB as failed rather than
unmounting another process's mount.

Sharing the same remote storage is fine for disciplined write patterns, but use
different local mountpoints per concurrent AKB process.

Safe: same remote storage, different local mountpoints.

```json
[
  {
    "rclone_remote": ":s3,env_auth=true,region=eu-west-1:my-bucket/shared-kb/",
    "mount": "$HOME/project-a/.akb/shared-kb"
  },
  {
    "rclone_remote": ":s3,env_auth=true,region=eu-west-1:my-bucket/shared-kb/",
    "mount": "$HOME/project-b/.akb/shared-kb"
  }
]
```

Unsafe: same remote storage and same local mountpoint.

```json
[
  {
    "rclone_remote": ":s3,env_auth=true,region=eu-west-1:my-bucket/shared-kb/",
    "mount": "$HOME/.akb/mounts/shared-kb"
  },
  {
    "rclone_remote": ":s3,env_auth=true,region=eu-west-1:my-bucket/shared-kb/",
    "mount": "$HOME/.akb/mounts/shared-kb"
  }
]
```

There is no automatic remount retry loop. If a KB fails because the local
mountpoint is already mounted elsewhere, stop the other owner process, choose a
unique mount path, then call `use_kb` with `action: "mount"` or restart the MCP
server.

### Default rclone args

These are applied to every mount unless overridden via `rclone_args`:

| Flag | Default | Purpose |
|------|---------|---------|
| `vfs-cache-mode` | `full` | Full read/write caching |
| `vfs-cache-max-size` | `1G` | Max local cache size |
| `vfs-cache-max-age` | `48h` | Cache entry TTL |
| `dir-cache-time` | `30s` | Directory listing cache |
| `poll-interval` | `15s` | Remote change polling |
| `vfs-write-back` | `5s` | Delay before flushing writes to remote |

On macOS, AKB also passes rclone's Apple metadata suppression flags where supported (`noappledouble`, `noapplexattr`) and removes disposable `._*` / `.DS_Store` artifacts before remote sync and graceful unmount.

AKB runs rclone as a tracked child process, so `daemon` is not supported in `rclone_args`.

### KB backups

Backups are disabled by default. Enable them when creating or patching a KB with `backup_enabled: true`; set `backup_keep` to control how many normal backup archives are retained.

`use_kb` with `action: "backup"` writes a compressed sibling archive next to the mount path, not inside it:

```text
/path/to/kb.20260512-130000.backup.tar.gz
```

After a successful backup, AKB prunes older normal backup archives and keeps the newest `backup.keep` files. Backup archives include dotfiles but skip disposable macOS metadata files such as `.DS_Store` and `._*`.

`use_kb` with `action: "restore"` restores from the latest retained normal backup. Before replacing contents, it creates a safety archive of the current KB contents:

```text
/path/to/kb.20260512-130500.pre-restore.backup.tar.gz
```

Restore deletes entries inside the KB root and then extracts the archive. For remote KBs, backup requires the KB to be mounted, waits for rclone write-back before archiving, and restore waits for rclone write-back after extracting. As with `sync`, this wait verifies the local mount process and write-back window; it is not a confirmed object-store commit.

## Prompts

Prompts are auto-discovered from `*.prompt.md` files in mounted KBs and registered as MCP prompts. Drop a file into any KB and it becomes available as a slash-command — no server restart required.

### File format

```markdown
---
description: Code review with project conventions
arguments:
  - name: language
    description: The programming language
    required: true
  - name: focus
    description: Specific areas to focus on
---

Review my {{.language}} code.{{if .focus}} Focus on: {{.focus}}{{end}}
```

See [examples/hello-world.prompt.md](examples/hello-world.prompt.md) for a complete example.

### Naming

The prompt name is derived from the file path relative to the KB root, minus the `.prompt.md` suffix, prefixed with the KB name:

| File path | KB name | Prompt name |
|-----------|---------|-------------|
| `lint.prompt.md` | `my-kb` | `my-kb/lint` |
| `go/review.prompt.md` | `my-kb` | `my-kb/go/review` |

### Template syntax

The body uses Go [`text/template`](https://pkg.go.dev/text/template) syntax:

- **Substitution**: `{{.language}}` inserts the argument value
- **Conditionals**: `{{if .focus}}...{{end}}` renders the block only when the argument is provided
- **Include**: `{{include "relative/path.md"}}` inlines another file

### Role delimiters

By default the entire body is a single `user` message. To create multi-message prompts, use `@role` headers (any heading level):

```markdown
## @assistant

You are a senior {{.language}} engineer.

## @user

Review the code I'm about to share.
```

## Operational notes

### Async startup mounts

KBs are mounted in a background goroutine that runs **concurrently** with the MCP server. Tools are available immediately, but remote KBs may not yet be mounted when the first request arrives. Use `use_kb` to mount explicitly if needed.

### Agent discovery

Agents should read `akb://kbs` as the single discovery resource. It includes each KB's config fields, `resolved_mount_path`, this server's live `mount_status`, optional `mount_error`, and remote `rclone_durability` timings.

Only use standard file tools on KBs whose `mount_status` is `"mounted"`.

### Remote KB durability

Remote KB writes go through rclone's VFS cache before reaching the object store. By default, `vfs-write-back` is `5s`.

After writing to a remote KB, call `use_kb` with `action: "sync"`. This waits for the effective write-back window plus a small grace buffer and verifies that the tracked rclone mount process is still healthy. It is not a confirmed S3/object-store commit.

When AKB exits normally because stdio closes, `SIGINT`, or `SIGTERM`, it also gives remote mounts the same bounded write-back grace before unmounting. Hard process death such as `SIGKILL` cannot run cleanup.

Remote changes made from another host may take roughly `poll-interval` / `dir-cache-time` to appear locally. Object stores are last-writer-wins for shared files; use unique append-only files for multi-agent records instead of concurrent appends to the same object.

### Config backend concurrency

| Backend | Concurrent-write behaviour |
|---------|---------------------------|
| **S3** | Uses `If-Match` / ETag for optimistic locking. Conflicts return `ErrConflict` — callers should re-`Retrieve` and retry. |
| **localfs** | No locking. Last-writer-wins. Intended for single-process use. |

### Docker wrappers

`bin/akb-docker-s3.sh` and `bin/akb-docker-local.sh` run the MCP server inside a Docker container. They mount `tmp/docker/` (relative to the repo root) as a volume at `/tmp/docker` inside the container and pass `--privileged`.

**Local KBs** (no `rclone_remote`) work normally — set `mount` to `/tmp/docker/<name>` so files are accessible on the host at `tmp/docker/<name>/`.

**Limitation — remote KBs are not supported on macOS with the Docker wrappers.** Docker Desktop uses VirtioFS to share macOS paths into the Docker VM. VirtioFS-backed bind mounts are always `private` propagation, so neither FUSE nor NFS mounts started inside the container are visible from the host. Use the native binary (`bin/akb.sh`) when `rclone_remote` is needed. See [docs/rclone-setup.md](docs/rclone-setup.md) for details.

### Prompt `include` security

`{{include "path"}}` is not sandboxed — a `../` traversal can read any file the server process can access. KB prompts are treated as trusted content.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code style, and the PR process.

## Development

See [AGENTS.md](AGENTS.md) for build commands, architecture, and code conventions.
