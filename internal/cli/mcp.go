package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/hookio"
	"github.com/gray0128/memctl/internal/mcp"
)

// runMCP implements `mcp --agent NAME [--sid ID]`: it serves the memory tools
// over newline-delimited JSON-RPC on stdin and stdout until stdin closes or the
// process is interrupted. The host starts it as a child process, so the tools
// run with the host's file permissions instead of the agent's bash sandbox.
// See docs/09-mcp-memory-tools.md.
func runMCP(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	agent := ""
	sid := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--agent" || arg == "--sid":
			if index+1 >= len(args) {
				return usageError(stderr, "mcp", arg+" requires a value")
			}
			index++
			if arg == "--agent" {
				agent = args[index]
			} else {
				sid = args[index]
			}
		case strings.HasPrefix(arg, "--agent="):
			agent = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--sid="):
			sid = strings.TrimPrefix(arg, "--sid=")
		default:
			return usageError(stderr, "mcp", fmt.Sprintf("unknown argument %q", arg))
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		// A broken configuration must still not corrupt the protocol stream:
		// report on stderr and exit.
		return fail(stderr, "mcp", err)
	}
	service := app.New(cfg)
	if cfg.BackgroundSync {
		service.EnableBackgroundSync()
	}

	mcp.SetServerVersion(version)
	server := mcp.NewServer(service, mcp.Session{
		Agent:     hookio.ResolveAgent(agent, os.Getenv),
		SessionID: hookio.ResolveSID(sid, os.Getenv, hookio.Input{}),
		Device:    hookio.ResolveDevice(os.Getenv),
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcp.Serve(ctx, server, stdin, stdout, stderr); err != nil {
		return fail(stderr, "mcp", err)
	}
	return ExitOK
}
