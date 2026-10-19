package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/server"
)

// DefaultUIPort is the documented default port of docs/04-ui-and-cli-specification.md
// section 5. Port 0 is accepted for tests and binds an ephemeral loopback port.
const DefaultUIPort = 18420

type uiOptions struct {
	port     int
	noOpen   bool
	readOnly bool
}

func parseUIOptions(args []string) (uiOptions, error) {
	options := uiOptions{port: DefaultUIPort}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--no-open":
			options.noOpen = true
		case arg == "--readonly":
			options.readOnly = true
		case arg == "--port":
			if index+1 >= len(args) {
				return options, memerr.New(memerr.InvalidArgument, "--port requires a value")
			}
			index++
			port, err := parseUIPort(args[index])
			if err != nil {
				return options, err
			}
			options.port = port
		case strings.HasPrefix(arg, "--port="):
			port, err := parseUIPort(strings.TrimPrefix(arg, "--port="))
			if err != nil {
				return options, err
			}
			options.port = port
		default:
			return options, memerr.New(memerr.InvalidArgument, "unknown argument %q", arg)
		}
	}
	return options, nil
}

func parseUIPort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 0 || port > 65535 {
		return 0, memerr.New(memerr.InvalidArgument,
			"invalid --port %q: want an integer from 0 to 65535", value)
	}
	return port, nil
}

// runUI implements `ui [--port N] [--no-open] [--readonly]`. It binds the
// documented loopback port (or an ephemeral loopback port for --port 0),
// serves the embedded frontend plus the read-only API, watches the repository
// for external edits and runs until interrupted.
func runUI(args []string, stdout, stderr io.Writer, version string) int {
	options, err := parseUIOptions(args)
	if err != nil {
		message := err.Error()
		if contractErr, ok := memerr.As(err); ok {
			message = contractErr.Message
		}
		return usageError(stderr, "ui", message)
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "ui", err)
	}

	// Bind before building the server so an ephemeral --port 0 address is the
	// one used for Host validation; the printed URL is always the real address.
	requested := fmt.Sprintf("127.0.0.1:%d", options.port)
	listener, err := server.Listen(requested)
	if err != nil {
		return fail(stderr, "ui", memerr.Wrap(memerr.IOFailed, err,
			"cannot listen on %s; is another memctl ui already running?", requested))
	}
	addr := listener.Addr().String()

	application := app.New(cfg)
	application.SetVersion(version)
	// The UI server runs no hook session, so its own commits (an applied
	// proposal, an audit revert) record memctl plus this device.
	application.SetCommitSource(app.CommitSource(os.Getenv))
	srv, err := server.New(server.Options{
		App:      application,
		Addr:     addr,
		ReadOnly: options.readOnly,
	})
	if err != nil {
		_ = listener.Close()
		return fail(stderr, "ui", err)
	}
	if err := srv.UIReady(); err != nil {
		_ = listener.Close()
		return fail(stderr, "ui", err)
	}

	httpServer := srv.NewHTTPServer()
	// Request contexts derive from baseCtx so shutdown can cancel an open SSE
	// stream (or any in-flight request) before waiting; http.Server.Shutdown
	// alone would block until the 5s budget expires on a long-lived stream.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	httpServer.BaseContext = func(net.Listener) context.Context { return baseCtx }
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Watch(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(stderr, "memctl ui: file watching is unavailable: %v\n", err)
		}
	}()
	go func() { _ = httpServer.Serve(listener) }()

	url := fmt.Sprintf("http://%s/", addr)
	fmt.Fprintf(stdout, "memctl ui: serving %s (read-only: %t)\n", url, options.readOnly)
	if !options.noOpen {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(stderr, "memctl ui: could not open a browser automatically: %v; open %s manually\n", err, url)
		}
	}

	<-ctx.Done()
	cancelBase()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fail(stderr, "ui", memerr.Wrap(memerr.IOFailed, err, "cannot shut down the UI server: %v", err))
	}
	return ExitOK
}
