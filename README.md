# agentic-knowledge-base

An MCP server that manages knowledge bases as mounted directories. AI agents interact with KBs using standard file tools (Read, Write, Glob, Grep) on local paths — remote storage is transparently mounted via [rclone](https://rclone.org/).

## Quick start

```bash
# local config backend
./bin/stdio.sh local

# S3 config backend (uses env credentials)
./bin/stdio.sh s3
```

## Prerequisites

- **Go 1.25+**
- **rclone** (only needed if you use remote-backed KBs)

### Installing rclone

> **Do not install rclone via Homebrew on macOS.** The Homebrew formula builds
> rclone without CGO (`CGO_ENABLED=0`), which strips out the `cmount` build tag.
> This means `rclone mount` (FUSE) **does not work** — it will error with:
>
> ```
> rclone mount is not supported on MacOS when rclone is installed via Homebrew
> ```
>
> `rclone nfsmount` (NFS) does work with the Homebrew build, but for full
> mount method support, use the official binary.

**Recommended:** download the official binary from [rclone.org/downloads](https://rclone.org/downloads/):

```bash
mkdir -p ~/.bin

# macOS (Apple Silicon)
curl -O https://downloads.rclone.org/rclone-current-osx-arm64.zip
unzip rclone-current-osx-arm64.zip
cp rclone-*-osx-arm64/rclone ~/.bin/
chmod +x ~/.bin/rclone

# macOS (Intel)
curl -O https://downloads.rclone.org/rclone-current-osx-amd64.zip
unzip rclone-current-osx-amd64.zip
cp rclone-*-osx-amd64/rclone ~/.bin/
chmod +x ~/.bin/rclone

# Linux
curl -O https://downloads.rclone.org/rclone-current-linux-amd64.zip
unzip rclone-current-linux-amd64.zip
cp rclone-*-linux-amd64/rclone ~/.bin/
chmod +x ~/.bin/rclone
```

Make sure `~/.bin` is on your `PATH` (add to `~/.zshrc` or `~/.bashrc`):

```bash
export PATH="$HOME/.bin:$PATH"
```

If you already have the Homebrew version installed, the official binary in
`~/.bin` will take precedence as long as `~/.bin` appears before
`/opt/homebrew/bin` in your `$PATH`.

## Mount methods

Remote KBs can be mounted using two strategies, controlled by the
`mount_method` field in the KB config:

| Value | rclone command | FUSE required | Notes |
|-------|---------------|--------------|-------|
| `""` (auto) | depends | depends | Prefers FUSE if available, falls back to NFS |
| `"fuse"` | `rclone mount` | **yes** | Best performance; requires a FUSE library |
| `"nfs"` | `rclone nfsmount` | **no** | Zero dependencies beyond rclone; slower writes on macOS |

### FUSE on macOS

On macOS there are two FUSE implementations. rclone (via the [cgofuse](https://github.com/winfsp/cgofuse)
library) probes them in this fixed order:

1. `/usr/local/lib/libfuse.2.dylib` — **macFUSE** (kernel extension, requires System Settings approval + reboot)
2. `/usr/local/lib/libosxfuse.2.dylib` — osxfuse legacy
3. `/usr/local/lib/libfuse-t.dylib` — **FUSE-T** (userspace via NFS, no kernel extension)

**You cannot select between them at runtime.** cgofuse loads the first dylib
it finds. This creates a problem when both are installed:

- If macFUSE's dylib exists but its kernel extension hasn't been authorized
  in System Settings → Privacy & Security, `rclone mount` will **fail** even
  though FUSE-T is also installed — because macFUSE's dylib is found first.
- The fix is to **uninstall macFUSE** if you want to use FUSE-T, or
  **authorize the macFUSE kext** in System Settings.

**Recommendation:** install FUSE-T only (`brew install --cask fuse-t`) — it
doesn't require kernel extensions or reboots.

### FUSE on Linux

Install FUSE 3:

```bash
sudo apt install fuse3   # Debian/Ubuntu
sudo dnf install fuse3   # Fedora
```

### NFS mount (no FUSE)

`rclone nfsmount` spins up a local NFS server and mounts it — no FUSE library
needed at all. This is the zero-setup option that works everywhere, including
with the Homebrew rclone build. Trade-off: writes are significantly slower on
macOS (~60s vs sub-second for FUSE-T).

The `auto` mount method (the default when `mount_method` is omitted) handles
this transparently: it checks for FUSE availability and falls back to NFS if
FUSE is not installed.

## KB configuration

Each KB entry in the config has these fields:

```json
{
  "rclone_remote": ":s3,provider=AWS,env_auth=true,region=eu-west-1:my-bucket/prefix/",
  "mount": "$HOME/.akb/mounts/my-kb",
  "mount_method": "nfs",
  "rclone_args": {
    "vfs-cache-max-size": "5G",
    "read-only": ""
  },
  "description": "My knowledge base"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `mount` | yes | Local directory path. FUSE/NFS mountpoint when `rclone_remote` is set, otherwise a plain local directory. |
| `rclone_remote` | no | rclone remote spec. Omit for a plain local directory. Format: `:backend,opt=val:bucket/path`. See [rclone docs](https://rclone.org/overview/#syntax-of-remote-paths). |
| `mount_method` | no | `"fuse"`, `"nfs"`, or omit for auto. |
| `rclone_args` | no | Flag overrides as `{"flag-name": "value"}` (without `--` prefix). Empty value for boolean flags. Merged on top of defaults. |
| `description` | no | Human-readable description. |

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
| `daemon` | *(bool)* | Run mount in background |
| `daemon-wait` | `30s` | Max time to wait for daemon startup |

## Prompts

Prompts are auto-discovered from `*.prompt.md` files in mounted KBs and registered as MCP prompts. Drop a file into any KB and it becomes available as a slash-command — no server restart required (file watchers handle live add/remove).

### File format

A `.prompt.md` file has optional YAML frontmatter and a markdown body:

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

| Frontmatter field | Required | Description |
|-------------------|----------|-------------|
| `description` | no | Human-readable prompt description |
| `arguments` | no | List of arguments with `name`, `description`, and `required` |

### Naming

The prompt name is derived from the file path relative to the KB root, minus the `.prompt.md` suffix, prefixed with the KB name:

| File path | KB name | Prompt name |
|-----------|---------|-------------|
| `lint.prompt.md` | `my-kb` | `my-kb/lint` |
| `go/review.prompt.md` | `my-kb` | `my-kb/go/review` |

### Template syntax

The body uses Go [`text/template`](https://pkg.go.dev/text/template) syntax. Arguments from the frontmatter are available as `{{.argName}}`:

- **Substitution**: `{{.language}}` inserts the argument value
- **Conditionals**: `{{if .focus}}...{{end}}` renders the block only when the argument is provided
- **Include**: `{{include "relative/path.md"}}` inlines another file, resolved relative to the prompt file's directory

Include example — reference a shared conventions file:

```markdown
---
description: Review code using project conventions
arguments:
  - name: language
    description: The programming language
    required: true
---

## @user

Review my {{.language}} code following these conventions:

{{include "conventions.md"}}
```

### Role delimiters

By default the entire body is a single `user` message. To create multi-message prompts, use `@role` headers (any heading level):

```markdown
## @assistant

You are a senior {{.language}} engineer performing a thorough code review.

## @user

Review the code I'm about to share. Look for bugs and performance issues.
```

MCP supports two roles: `user` and `assistant`.

## Operational notes

### Async startup mounts

KBs are mounted in a background goroutine that runs **concurrently** with the
MCP server. Tools (`list_kbs`, file reads/writes) are available immediately at
startup, but remote KBs may not yet be mounted when the first request arrives.
If a remote KB shows `mounted=false` right after startup, wait a moment and
retry, or use `use_kb` to mount it explicitly.

Mount failures are logged to stderr but do not prevent the server from starting.

### Config backend concurrency

| Backend | Concurrent-write behaviour |
|---------|---------------------------|
| **S3** | Uses `If-Match` / ETag for optimistic locking. Concurrent writes detect conflicts and return `ErrConflict` — callers should re-`Retrieve` and retry. |
| **localfs** | No locking. Concurrent writes from multiple processes use **last-writer-wins** semantics and can silently overwrite each other. The localfs backend is intended for single-process use. |

### Prompt `include` security

The `{{include "path"}}` template function resolves paths **relative to the
prompt file's directory** and is not sandboxed. A prompt that uses `../` can
read any file the server process can access. KB prompts are treated as trusted
content — do not store untrusted prompt files in a KB.

## Development

See [AGENTS.md](AGENTS.md) for build commands, architecture, and code conventions.
