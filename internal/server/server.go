// Package server is the local read-only HTTP surface of memctl. It registers
// the documented REST routes, enforces the HTTP security rules of
// docs/04-ui-and-cli-specification.md section 5 and converts one application
// request into one JSON response. It never takes its own repository lock; the
// application service owns locking.
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/watcher"
)

// DefaultAddr is the documented UI address. The service only ever binds the
// loopback address of this port.
const DefaultAddr = "127.0.0.1:18420"

// DefaultMaxRequestBytes is the documented single JSON request limit from
// docs/02-domain-models-and-contracts.md section 4.1.
const DefaultMaxRequestBytes = 8 << 20

// HTTP-only error codes that are not part of the CLI/Git error contract.
const (
	codeAuthRequired = "AUTH_REQUIRED"
	codeOriginDenied = "ORIGIN_DENIED"
	codeReadOnly     = "READ_ONLY"
)

// Options configures one Server.
type Options struct {
	// App is the application service every route calls. It is required.
	App *app.App
	// Addr is the loopback host and port used for Host validation. It defaults
	// to DefaultAddr.
	Addr string
	// ReadOnly rejects every non-GET/HEAD API request in the server, not only
	// in the UI.
	ReadOnly bool
	// Token is the session token. An empty value generates a new 32-byte
	// random token; tests set it explicitly.
	Token string
	// MaxRequestBytes bounds one request body. Zero uses DefaultMaxRequestBytes.
	MaxRequestBytes int64
	// Events is the invalidation fan-out used by GET /api/v1/events. A nil
	// value creates a bus with DefaultSSEQueue. Tests inject a smaller queue.
	Events *watcher.Bus
	// SSEHeartbeat is the comment interval of the SSE stream. Zero uses
	// DefaultSSEHeartbeat. Tests use a short interval.
	SSEHeartbeat time.Duration
}

// Server holds the routes, session token and address policy.
type Server struct {
	app             *app.App
	addr            string
	readOnly        bool
	token           string
	maxRequestBytes int64
	allowedHosts    map[string]struct{}
	allowedOrigins  map[string]struct{}
	events          *watcher.Bus
	sseHeartbeat    time.Duration
	mux             *http.ServeMux
}

// New builds a Server and registers every route. It does not listen.
func New(opts Options) (*Server, error) {
	if opts.App == nil {
		return nil, memerr.New(memerr.InvalidArgument, "server requires an application service")
	}
	addr := opts.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	host, port, err := splitAddr(addr)
	if err != nil {
		return nil, err
	}
	token := opts.Token
	if token == "" {
		token, err = generateToken()
		if err != nil {
			return nil, err
		}
	}
	maxRequestBytes := opts.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = DefaultMaxRequestBytes
	}
	events := opts.Events
	if events == nil {
		events = watcher.NewBus(DefaultSSEQueue)
	}
	sseHeartbeat := opts.SSEHeartbeat
	if sseHeartbeat <= 0 {
		sseHeartbeat = DefaultSSEHeartbeat
	}

	server := &Server{
		app:             opts.App,
		addr:            net.JoinHostPort(host, port),
		readOnly:        opts.ReadOnly,
		token:           token,
		maxRequestBytes: maxRequestBytes,
		allowedHosts:    map[string]struct{}{},
		allowedOrigins:  map[string]struct{}{},
		events:          events,
		sseHeartbeat:    sseHeartbeat,
	}
	for _, name := range []string{"127.0.0.1", "localhost"} {
		server.allowedHosts[net.JoinHostPort(name, port)] = struct{}{}
		server.allowedOrigins["http://"+net.JoinHostPort(name, port)] = struct{}{}
	}
	server.registerRoutes()
	// A committed local write invalidates the client view immediately; the
	// file watcher only covers external edits. See
	// docs/02-domain-models-and-contracts.md section 5.
	opts.App.SetMutationObserver(func(event app.MutationEvent) {
		server.events.Publish(watcher.Invalidation{
			Reason:       watcher.ReasonVaultUpdated,
			ChangedFiles: event.ChangedFiles,
			Revision:     event.Revision,
		})
	})
	return server, nil
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler { return s.security(s.mux) }

// NewHTTPServer returns a configured http.Server for this handler. It bounds
// header read time and idle connections as required by
// docs/04-ui-and-cli-specification.md section 5. It deliberately sets no
// write timeout, so the long-lived SSE stream added in B-2 is not killed by a
// whole-response deadline.
func (s *Server) NewHTTPServer() *http.Server {
	return &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// Token returns the session token generated for this server.
func (s *Server) Token() string { return s.token }

// Addr returns the normalized loopback address.
func (s *Server) Addr() string { return s.addr }

// ReadOnly reports whether the server rejects write requests.
func (s *Server) ReadOnly() bool { return s.readOnly }

// Listen binds the documented loopback address. It refuses any non-loopback
// host instead of silently exposing the service.
func Listen(addr string) (net.Listener, error) {
	host, port, err := splitAddr(addr)
	if err != nil {
		return nil, err
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, memerr.New(memerr.InvalidArgument,
			"memctl only listens on the loopback address, not %q", host)
	}
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
}

func (s *Server) registerRoutes() {
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /api/v1/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("GET /api/v1/files/{path...}", s.handleFile)
	s.mux.HandleFunc("GET /api/v1/entries", s.handleEntries)
	s.mux.HandleFunc("GET /api/v1/audit", s.handleAudit)
	s.mux.HandleFunc("GET /api/v1/doctor", s.handleDoctor)
	s.mux.HandleFunc("GET /api/v1/conflict", s.handleConflict)
	s.mux.HandleFunc("GET /api/v1/events", s.handleEvents)
}

// security applies the HTTP security rules to every request in the documented
// order: response headers, Host, Origin, session token, path validity and the
// read-only gate.
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		if _, ok := s.allowedHosts[r.Host]; !ok {
			writeError(w, http.StatusBadRequest, string(memerr.InvalidArgument),
				fmt.Sprintf("request Host %q is not the loopback address this server listens on", r.Host), false)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if _, ok := s.allowedOrigins[origin]; !ok {
				writeError(w, http.StatusForbidden, codeOriginDenied,
					"cross-origin API requests are not allowed", false)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !s.tokenMatches(r.Header.Get("X-Memctl-Token")) {
				writeError(w, http.StatusUnauthorized, codeAuthRequired,
					"a valid X-Memctl-Token header is required", false)
				return
			}
			if err := validateRequestPath(r.URL.Path); err != nil {
				s.writeAppError(w, err)
				return
			}
		}
		if s.readOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusForbidden, codeReadOnly,
				"the server is running in read-only mode; write requests are rejected", false)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) tokenMatches(provided string) bool {
	if provided == "" || len(provided) != len(s.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func setSecurityHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
}

// validateRequestPath rejects traversal aliases before ServeMux can normalize
// them, so the unified repository boundary returns INVALID_PATH instead of a
// redirect into another route.
func validateRequestPath(path string) error {
	if strings.ContainsRune(path, 0) {
		return memerr.New(memerr.InvalidPath, "request path must not contain NUL")
	}
	if strings.ContainsRune(path, '\\') {
		return memerr.New(memerr.InvalidPath, "request path must use / and must not contain backslashes")
	}
	for index, segment := range strings.Split(path, "/") {
		if index == 0 {
			// The leading empty segment comes from the absolute request path.
			continue
		}
		if segment == "" {
			return memerr.New(memerr.InvalidPath, "request path must not contain empty segments")
		}
		if segment == "." || segment == ".." {
			return memerr.New(memerr.InvalidPath, "request path must not contain %q segments", segment)
		}
	}
	return nil
}

// splitAddr parses "host:port" and rejects a missing or non-loopback host.
func splitAddr(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Allow a bare port for convenience, as in Addr: "18420".
		if value, convErr := strconv.Atoi(addr); convErr == nil && value >= 0 {
			return "127.0.0.1", addr, nil
		}
		return "", "", memerr.New(memerr.InvalidArgument, "invalid listen address %q", addr)
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return "", "", memerr.New(memerr.InvalidArgument,
			"memctl only listens on the loopback address, not %q", host)
	}
	if port == "" {
		return "", "", memerr.New(memerr.InvalidArgument, "listen address %q has no port", addr)
	}
	return host, port, nil
}

func generateToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot generate the session token")
	}
	return hex.EncodeToString(raw), nil
}
