package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bkosm/akb/go/akb/config"
	configlocalfs "github.com/bkosm/akb/go/akb/config/adapter/localfs"
	configs3 "github.com/bkosm/akb/go/akb/config/adapter/s3"
	"github.com/bkosm/akb/go/akb/endpoints"
	endpointconfig "github.com/bkosm/akb/go/akb/endpoints/config"
	"github.com/bkosm/akb/go/akb/endpoints/kbs"
	"github.com/bkosm/akb/go/akb/endpoints/listkbs"
	"github.com/bkosm/akb/go/akb/endpoints/newkb"
	"github.com/bkosm/akb/go/akb/endpoints/patchkb"
	"github.com/bkosm/akb/go/akb/endpoints/purgekb"
	"github.com/bkosm/akb/go/akb/endpoints/usekb"
	"github.com/bkosm/akb/go/akb/filewatch"
	"github.com/bkosm/akb/go/akb/mount"
	"github.com/bkosm/akb/go/akb/prompt"
)

// version is set at build time via -ldflags "-X main.version=<version>".
var version = "dev"

const serverInstructionsBase = `AKB (Agentic Knowledge Base) is a remote knowledge base orchestrator for cross-repo and cross-host agent knowledge sharing.

It mounts local or remote directories (backed by any rclone-supported storage: S3, GCS, SFTP, etc.) so agents can read and write knowledge using standard file tools.

Workflow:
  1. Read the akb://kbs resource to discover available KBs, their mount paths, and whether each is local or remote-backed.
  2. Use standard file tools (Read, Write, Glob, Grep) on the returned mount paths — all configured KBs are auto-mounted on server startup.
  3. Use new_kb to register additional knowledge bases (local directories or remote storage).

Two independent dimensions:
  - Config backend: where the KB registry (list of KBs) is stored — either a local file or an S3 object.
  - KB storage: where each KB's files actually live — either a local directory or a rclone remote (S3, GCS, SFTP, …).
  Any combination is valid. A local-config server can have S3-backed KBs; an S3-config server can have local-directory KBs.

Prompts are auto-discovered from *.prompt.md files in KBs. Write a .prompt.md file to any KB and it becomes a slash-command prompt automatically.

The use_kb tool is only needed for troubleshooting — e.g. re-mounting a KB that failed at startup or manually unmounting to free resources.

Use patch_kb to update KB connection settings. Changes to config take effect after MCP server restart.
Use purge_kb to remove a KB from config, optionally deleting all files at its mount path.`

// buildServerInstructions returns the MCP server instructions, appending a
// config-backend identity line so agents can distinguish server instances.
func buildServerInstructions(backendInfo string) string {
	if backendInfo == "" {
		return serverInstructionsBase
	}
	return serverInstructionsBase + fmt.Sprintf("\n\nConfig backend: %s", backendInfo)
}

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	level := slog.LevelInfo
	switch logLevel {
	case "debug", "DEBUG":
		level = slog.LevelDebug
	case "warn", "WARN":
		level = slog.LevelWarn
	case "error", "ERROR":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: akb <local|s3> [flags]")
		os.Exit(1)
	}

	ctx := context.Background()
	var configurer config.Interface

	switch os.Args[1] {
	case "local":
		fs := flag.NewFlagSet("local", flag.ExitOnError)
		path := fs.String("path", "$HOME/.config/akb/config.json", "config file path")
		if err := fs.Parse(os.Args[2:]); err != nil {
			slog.Error("parse flags", "err", err)
			os.Exit(1)
		}
		configurer = &configlocalfs.LocalFS{Path: *path}

	case "s3":
		fs := flag.NewFlagSet("s3", flag.ExitOnError)
		bucket := fs.String("bucket", "", "S3 bucket name (default: akb-<account-id>)")
		region := fs.String("region", "", "AWS region (optional)")
		configKey := fs.String("config-key", "config.json", "S3 object key for config")
		if err := fs.Parse(os.Args[2:]); err != nil {
			slog.Error("parse flags", "err", err)
			os.Exit(1)
		}

		awsCfg, err := configs3.LoadConfig(ctx, *region)
		if err != nil {
			slog.Error("load AWS config", "err", err)
			os.Exit(1)
		}
		s3Cfg, err := configs3.New(ctx, *bucket, *configKey, awsCfg)
		if err != nil {
			slog.Error("init S3 config backend", "err", err)
			os.Exit(1)
		}
		configurer = s3Cfg

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	backendInfo := ""
	if bd, ok := configurer.(config.BackendDescriber); ok {
		backendInfo = bd.BackendInfo()
	}

	if err := run(ctx, configurer, backendInfo, &mcp.StdioTransport{}); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configurer config.Interface, backendInfo string, transport mcp.Transport) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ctx = config.IntoContext(ctx, configurer)

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return err
	}

	mgr := mount.NewManager()
	ctx = mount.ManagerIntoContext(ctx, mgr)

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "akb",
			Title:   "Agentic Knowledge Base",
			Version: version,
		},
		&mcp.ServerOptions{
			Instructions: buildServerInstructions(backendInfo),
			Capabilities: &mcp.ServerCapabilities{
				Logging:   &mcp.LoggingCapabilities{},
				Prompts:   &mcp.PromptCapabilities{ListChanged: true},
				Resources: &mcp.ResourceCapabilities{ListChanged: true},
			},
		},
	)

	for _, register := range []endpoints.RegisterFunc{
		endpointconfig.Register,
		kbs.Register,
		newkb.Register,
		listkbs.Register,
		patchkb.Register,
		purgekb.Register,
		usekb.Register,
	} {
		if err := register(ctx, server); err != nil {
			return err
		}
	}

	ctx = mount.OnMountedIntoContext(ctx, func(kbName, mountPath string) (func(), error) {
		return filewatch.Register(mountPath, prompt.PromptSuffix, prompt.NewHandler(server, kbName))
	})

	slog.Info("akb starting", "kbs", len(cfg.KBs))

	startMounts, cleanup, err := mgr.ServeSetup(ctx, cfg.KBs)
	if err != nil {
		return err
	}
	defer cleanup()
	go startMounts()

	return server.Run(ctx, transport)
}
