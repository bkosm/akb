package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bkosm/akb/config"
	configlocalfs "github.com/bkosm/akb/config/adapter/localfs"
	configs3 "github.com/bkosm/akb/config/adapter/s3"
	"github.com/bkosm/akb/endpoints"
	endpointconfig "github.com/bkosm/akb/endpoints/config"
	"github.com/bkosm/akb/endpoints/listkbs"
	"github.com/bkosm/akb/endpoints/newkb"
	"github.com/bkosm/akb/endpoints/patchconfig"
	"github.com/bkosm/akb/endpoints/usekb"
	"github.com/bkosm/akb/mount"
	"github.com/bkosm/akb/prompt"
)

const serverInstructions = `AKB (Agentic Knowledge Base) is a remote knowledge base orchestrator for cross-repo and cross-host agent knowledge sharing.

It mounts local or remote directories (backed by any rclone-supported storage: S3, GCS, SFTP, etc.) so agents can read and write knowledge using standard file tools.

Workflow:
  1. Call list_kbs to discover available KBs and their mount paths.
  2. Use standard file tools (Read, Write, Glob, Grep) on the returned mount paths — all configured KBs are auto-mounted on server startup.
  3. Use new_kb to register additional knowledge bases (local directories or remote storage).

Prompts are auto-discovered from *.prompt.md files in KBs. Write a .prompt.md file to any KB and it becomes a slash-command prompt automatically.

The use_kb tool is only needed for troubleshooting — e.g. re-mounting a KB that failed at startup or manually unmounting to free resources.

Use patch_config to update KB connection settings. Changes to config take effect after MCP server restart.`

func main() {
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
		fs.Parse(os.Args[2:])
		configurer = &configlocalfs.LocalFS{Path: *path}

	case "s3":
		fs := flag.NewFlagSet("s3", flag.ExitOnError)
		bucket := fs.String("bucket", "", "S3 bucket name (default: akb-<account-id>)")
		region := fs.String("region", "", "AWS region (optional)")
		configKey := fs.String("config-key", "config.json", "S3 object key for config")
		fs.Parse(os.Args[2:])

		awsCfg, err := configs3.LoadConfig(ctx, *region)
		if err != nil {
			log.Fatal(err)
		}
		s3Cfg, err := configs3.New(ctx, *bucket, *configKey, awsCfg)
		if err != nil {
			log.Fatal(err)
		}
		configurer = s3Cfg

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	if err := run(ctx, configurer, &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, configurer config.Interface, transport mcp.Transport) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ctx = config.IntoContext(ctx, configurer)

	cfg, err := configurer.Retrieve(ctx)
	if err != nil {
		return err
	}

	mgr := mount.NewManager()
	ctx = mount.IntoContext(ctx, mgr)

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "akb",
			Title:   "Agentic Knowledge Base",
			Version: "0.0.1",
		},
		&mcp.ServerOptions{
			Instructions: serverInstructions,
			Capabilities: &mcp.ServerCapabilities{
				Logging: &mcp.LoggingCapabilities{},
				Prompts: &mcp.PromptCapabilities{ListChanged: true},
			},
		},
	)

	for _, register := range []endpoints.RegisterFunc{
		endpointconfig.Register,
		newkb.Register,
		listkbs.Register,
		patchconfig.Register,
		usekb.Register,
	} {
		if err := register(ctx, server); err != nil {
			return err
		}
	}

	run, cleanup, err := mgr.ServeSetup(cfg.KBs, func(name, mountPath string) func() {
		stop, err := prompt.RegisterForKB(server, name, mountPath)
		if err != nil {
			log.Printf("prompts: register for kb %q: %v", name, err)
			return nil
		}
		return stop
	})
	if err != nil {
		return err
	}
	defer cleanup()
	go run()

	return server.Run(ctx, transport)
}
