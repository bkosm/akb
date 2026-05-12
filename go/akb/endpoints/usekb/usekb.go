package usekb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bkosm/akb/go/akb/backup"
	"github.com/bkosm/akb/go/akb/config"
	"github.com/bkosm/akb/go/akb/endpoints"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Input holds the parameters for the use_kb tool.
type Input struct {
	Name   string `json:"name" jsonschema:"name of the knowledge base to mount, unmount, sync, backup, or restore"`
	Action string `json:"action" jsonschema:"'mount' to activate the KB, 'unmount' to deactivate it, 'sync' to wait for remote write-back, 'backup' to create a backup archive, or 'restore' to replace contents from the latest backup"`
}

// Output is the response payload for the use_kb tool.
type Output struct {
	Mount            string `json:"mount" jsonschema:"resolved local path"`
	Status           string `json:"status" jsonschema:"result of the action"`
	Assurance        string `json:"assurance,omitempty" jsonschema:"sync assurance level, present for sync actions"`
	Waited           string `json:"waited,omitempty" jsonschema:"duration waited for write-back, present for sync actions"`
	BackupPath       string `json:"backup_path,omitempty" jsonschema:"backup archive path created by backup or pre-restore safety backup"`
	RestoredFrom     string `json:"restored_from,omitempty" jsonschema:"backup archive used for restore"`
	SafetyBackupPath string `json:"safety_backup_path,omitempty" jsonschema:"pre-restore safety backup archive created during restore"`
	PrunedBackups    int    `json:"pruned_backups,omitempty" jsonschema:"number of old backup archives removed by retention pruning"`
	RetainedBackups  int    `json:"retained_backups,omitempty" jsonschema:"number of normal backup archives retained after pruning"`
}

// Handle implements the use_kb tool handler.
func Handle(ctx context.Context, req *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	if input.Name == "" {
		return nil, Output{}, fmt.Errorf("name is required")
	}
	if !validAction(input.Action) {
		return nil, Output{}, fmt.Errorf("action must be 'mount', 'unmount', 'sync', 'backup', or 'restore'")
	}

	configurer, err := config.FromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("retrieve config: %w", err)
	}

	kbEntry, ok := cfg.KBs[config.Unique(input.Name)]
	if !ok {
		return nil, Output{}, fmt.Errorf("knowledge base %q not found in config", input.Name)
	}

	resolved := filepath.Clean(os.ExpandEnv(kbEntry.Mount))

	if kbEntry.RcloneRemote == "" {
		switch input.Action {
		case "sync":
			return nil, Output{
				Mount:     resolved,
				Status:    fmt.Sprintf("local KB %q — no sync action needed", input.Name),
				Assurance: mount.SyncAssuranceLocalNoop,
				Waited:    "0s",
			}, nil
		case "backup":
			return handleBackup(input.Name, resolved, nil, kbEntry.Backup)
		case "restore":
			return handleRestore(input.Name, resolved, nil, kbEntry.Backup)
		default:
			return nil, Output{
				Mount:  resolved,
				Status: fmt.Sprintf("local KB %q — no mount action needed", input.Name),
			}, nil
		}
	}

	mgr, err := mount.ManagerFromContext(ctx)
	if err != nil {
		return nil, Output{}, fmt.Errorf("mount manager: %w", err)
	}

	switch input.Action {
	case "mount":
		if mgr.IsMounted(kbEntry.Mount) {
			return nil, Output{
				Mount:  resolved,
				Status: fmt.Sprintf("KB %q is already mounted at %s", input.Name, resolved),
			}, nil
		}

		if err := mgr.Preflight(); err != nil {
			return nil, Output{}, err
		}
		if err := mgr.Add(ctx, input.Name, kbEntry.RcloneRemote, kbEntry.Mount, mount.Method(kbEntry.Method), kbEntry.RcloneArgs); err != nil {
			return nil, Output{}, fmt.Errorf("mount: %w", err)
		}
		return nil, Output{
			Mount:  resolved,
			Status: fmt.Sprintf("KB %q mounted at %s", input.Name, resolved),
		}, nil

	case "unmount":
		unmountErr := mgr.Unmount(kbEntry.Mount)
		mgr.Deregister(kbEntry.Mount)
		if unmountErr != nil {
			return nil, Output{}, fmt.Errorf("unmount: %w", unmountErr)
		}
		return nil, Output{
			Mount:  resolved,
			Status: fmt.Sprintf("KB %q unmounted from %s", input.Name, resolved),
		}, nil

	case "sync":
		result, err := mgr.Sync(kbEntry.Mount)
		if err != nil {
			return nil, Output{}, fmt.Errorf("sync: %w", err)
		}
		return nil, Output{
			Mount:     resolved,
			Status:    fmt.Sprintf("KB %q sync completed at %s", input.Name, resolved),
			Assurance: result.Assurance,
			Waited:    result.Waited.String(),
		}, nil

	case "backup":
		if !mgr.IsMounted(kbEntry.Mount) {
			return nil, Output{}, fmt.Errorf("backup: remote KB %q is not mounted", input.Name)
		}
		return handleBackup(input.Name, resolved, mgr, kbEntry.Backup)

	case "restore":
		if !mgr.IsMounted(kbEntry.Mount) {
			return nil, Output{}, fmt.Errorf("restore: remote KB %q is not mounted", input.Name)
		}
		return handleRestore(input.Name, resolved, mgr, kbEntry.Backup)
	}

	return nil, Output{}, fmt.Errorf("unexpected action %q", input.Action)
}

func validAction(action string) bool {
	switch action {
	case "mount", "unmount", "sync", "backup", "restore":
		return true
	default:
		return false
	}
}

func handleBackup(name, resolved string, mgr *mount.Manager, settings *config.BackupSettings) (*mcp.CallToolResult, Output, error) {
	backupSettings, err := requireBackupEnabled(name, settings)
	if err != nil {
		return nil, Output{}, err
	}

	out := Output{Mount: resolved}
	if mgr != nil {
		result, err := mgr.Sync(resolved)
		if err != nil {
			return nil, Output{}, fmt.Errorf("sync before backup: %w", err)
		}
		out.Assurance = result.Assurance
		out.Waited = result.Waited.String()
	}

	archivePath, err := backup.Create(resolved, time.Now(), backup.KindNormal)
	if err != nil {
		return nil, Output{}, fmt.Errorf("backup: %w", err)
	}
	pruned, err := backup.Prune(resolved, backup.KindNormal, backupSettings.Keep)
	if err != nil {
		return nil, Output{}, fmt.Errorf("prune backups: %w", err)
	}
	retained, err := backup.List(resolved, backup.KindNormal)
	if err != nil {
		return nil, Output{}, fmt.Errorf("list backups: %w", err)
	}

	out.Status = fmt.Sprintf("KB %q backup created at %s", name, archivePath)
	out.BackupPath = archivePath
	out.PrunedBackups = pruned
	out.RetainedBackups = len(retained)
	return nil, out, nil
}

func handleRestore(name, resolved string, mgr *mount.Manager, settings *config.BackupSettings) (*mcp.CallToolResult, Output, error) {
	if _, err := requireBackupEnabled(name, settings); err != nil {
		return nil, Output{}, err
	}
	latest, err := backup.Latest(resolved)
	if err != nil {
		return nil, Output{}, fmt.Errorf("restore: %w", err)
	}
	safetyPath, err := backup.Create(resolved, time.Now(), backup.KindPreRestore)
	if err != nil {
		return nil, Output{}, fmt.Errorf("pre-restore backup: %w", err)
	}
	if err := backup.ReplaceContents(resolved, latest.Path); err != nil {
		return nil, Output{}, fmt.Errorf("restore: %w", err)
	}

	out := Output{
		Mount:            resolved,
		Status:           fmt.Sprintf("KB %q restored from %s", name, latest.Path),
		BackupPath:       safetyPath,
		RestoredFrom:     latest.Path,
		SafetyBackupPath: safetyPath,
	}
	if mgr != nil {
		result, err := mgr.Sync(resolved)
		if err != nil {
			return nil, Output{}, fmt.Errorf("sync after restore: %w", err)
		}
		out.Assurance = result.Assurance
		out.Waited = result.Waited.String()
	}
	return nil, out, nil
}

func requireBackupEnabled(name string, settings *config.BackupSettings) (*config.BackupSettings, error) {
	normalized := config.NormalizeBackup(settings)
	if normalized == nil || !normalized.Enabled {
		return nil, fmt.Errorf("backup is not enabled for KB %q", name)
	}
	return normalized, nil
}

const toolDescription = `Manually mount, unmount, sync, backup, or restore a knowledge base. Mount and unmount are troubleshooting actions — all KBs are auto-mounted at server startup, so you should not need them in normal operation.

Use cases:
  - Re-mount a remote KB that failed during startup
  - Unmount a remote KB to free system resources (rclone process, FUSE/NFS mount)
  - Create a timestamped sibling backup archive for a KB with backups enabled
  - Restore a KB from its latest retained backup archive

Actions:
  - "mount": activate a remote KB (starts rclone, makes files accessible at mount path)
  - "unmount": deactivate a remote KB (stops rclone, releases mountpoint)
  - "sync": after writing to a remote KB, wait for rclone's configured write-back window and verify the mount process remains healthy
  - "backup": create <mount>.YYYYMMDD-HHMMSS.backup.tar.gz alongside the mount path, then prune old backup archives according to backup.keep
  - "restore": create <mount>.YYYYMMDD-HHMMSS.pre-restore.backup.tar.gz, replace KB contents with the latest retained backup archive, then sync remote KBs

Local KBs (no rclone_remote) are always accessible — mount, unmount, and sync are no-ops. Backup and restore operate on the local directory.`

// Register adds the use_kb tool to the MCP server.
var Register endpoints.RegisterFunc = func(_ context.Context, s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "use_kb",
		Title:       "Mount / Unmount Knowledge Base",
		Description: toolDescription,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &endpoints.BoolTrue,
			IdempotentHint:  false,
			OpenWorldHint:   &endpoints.BoolFalse,
		},
	}, Handle)
	return nil
}
