AKB (Agentic Knowledge Base) is a remote knowledge base orchestrator for cross-repo and cross-host agent knowledge sharing.

It mounts local or remote directories (backed by any rclone-supported storage: S3, GCS, SFTP, etc.) so agents can read and write knowledge using standard file tools.

Workflow:
  1. Read the akb://kbs resource to discover available KBs, config fields, resolved mount paths, and live mount status.
  2. Use standard file tools (Read, Write, Glob, Grep) only on KBs whose mount_status is "mounted".
  3. After writing to a remote-backed KB, call use_kb with action "sync" to wait for rclone's write-back window and verify mount health.
  4. Use new_kb to register additional knowledge bases (local directories or remote storage).
  5. For KBs with backup.enabled=true, call use_kb with action "backup" to create a retained sibling archive, or action "restore" to replace contents from the latest retained backup.

Two independent dimensions:
  - Config backend: where the KB registry (list of KBs) is stored — either a local file or an S3 object.
  - KB storage: where each KB's files actually live — either a local directory or a rclone remote (S3, GCS, SFTP, …).
  Any combination is valid. A local-config server can have S3-backed KBs; an S3-config server can have local-directory KBs.

The akb://kbs resource is the single discovery resource for agents. It is a superset of raw KB config: it includes config fields, resolved_mount_path, mount_status, mount_error, and remote durability timings where applicable.

Prompts are auto-discovered from *.prompt.md files in KBs and become MCP prompts invokable by the user as slash commands.
A minimal prompt is a plain markdown file — the body becomes a single user message.
Add an optional YAML frontmatter block with a description and argument definitions.
Read akb://prompt-reference for the full authoring reference (multi-message, templates, include, naming).

Mount path convention:
  When creating a new KB scoped to the current project, set mount to .akb/<name> under the
  repository root (e.g. $HOME/my-repo/.akb/my-kb).
  Ensure .akb is listed in the repository's .gitignore so KB content is never committed.
  For global KBs shared across projects, $HOME/.akb/mounts/<name> is a good default.
  All config fields support $ENV_VAR expansion at runtime. When using a remote config backend
  (S3), always use env var prefixes like $HOME instead of bare absolute paths so configs stay
  portable across developers and machines. Local config backends can use absolute paths.

The use_kb tool is used for troubleshooting, remote write safety, and KB backups — e.g. re-mounting a KB that failed at startup, manually unmounting to free resources, action "sync" after writes to a remote KB, action "backup" to create a timestamped archive, or action "restore" to restore from the latest retained backup.

Backup workflow:
  - Backups are disabled by default. Enable them per KB with backup_enabled when creating or patching a KB; backup_keep defaults to 3.
  - use_kb action "backup" writes a compressed sibling archive outside the mount tree: <mount>.YYYYMMDD-HHMMSS.backup.tar.gz.
  - After a successful backup, older normal backup archives are pruned so only backup.keep newest normal backups remain.
  - use_kb action "restore" first creates a safety archive of current contents: <mount>.YYYYMMDD-HHMMSS.pre-restore.backup.tar.gz.
  - Restore then deletes entries inside the KB root and extracts the latest retained normal backup into the KB root.
  - A pre-restore safety archive is a manual rollback point for undoing the restore that just happened; it is only useful after restore has replaced the KB contents.
  - use_kb action "restore" never selects pre-restore safety archives automatically, and normal backup retention does not prune them.
  - For remote KBs, backup requires the KB to be mounted, waits for rclone write-back before archiving, and restore waits for rclone write-back after extraction.

Remote KB durability contract:
  - use_kb action "sync" is timer and mount-health based; it is not a confirmed S3/object-store commit.
  - rclone write-back defaults to 5s, so sync waits for the effective write-back window plus a small grace buffer.
  - remote changes from other hosts may take roughly poll-interval/dir-cache-time to appear locally.
  - shared-file writes are last-writer-wins on object stores; use unique append-only files for multi-agent records.
  - macOS metadata artifacts such as ._* and .DS_Store are disposable and may be cleaned from remote mounts.

Use patch_kb to update KB connection settings. Changes to config take effect after MCP server restart.
Use purge_kb to remove a KB from config, optionally deleting all files at its mount path.
