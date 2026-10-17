package mcp

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// DefaultEntryPoint is the file memory_read returns when no path is given.
const DefaultEntryPoint = vault.EntryPointName

// Service is the application surface the tools need. *app.App implements it.
type Service interface {
	ReadFile(ctx context.Context, relPath string) (vault.FileRead, error)
	Apply(ctx context.Context, req app.MutationRequest) (app.MutationResult, error)
}

// writeBudget bounds how long a busy repository lock may be retried. A model
// tool call should fail with BUSY rather than hold the session open.
const writeAttempts = 2

// Session carries the identity recorded on every write.
type Session struct {
	Agent     string
	SessionID string
	Device    string
}

// Server holds the tool definitions and the application service.
type Server struct {
	service Service
	session Session
}

// NewServer builds a server over one application service.
func NewServer(service Service, session Session) *Server {
	return &Server{service: service, session: session}
}

// Tools returns the tool list in a stable order.
func (s *Server) Tools() []Tool {
	return []Tool{
		{
			Name:        "memory_read",
			Description: "Read one file from the shared memory repository and return its fileRevision baseline. Read before every write. The default file is MEMORY.md.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Repository-relative path, for example MEMORY.md or notes/topic.md.",
					},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "memory_write",
			Description: "Write one file to the shared memory repository through the managed transaction. The base revision must come from memory_read; use absent to create a new file. A stale base is rejected instead of overwriting another update.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Repository-relative path of the file to write.",
					},
					"base": map[string]any{
						"type":        "string",
						"description": "fileRevision from memory_read, or absent when creating a new file.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Complete new file text. One bullet per entry, metadata at the end.",
					},
				},
				"required":             []string{"path", "base", "content"},
				"additionalProperties": false,
			},
		},
	}
}

// readArgs is the memory_read argument object.
type readArgs struct {
	Path string `json:"path"`
}

// writeArgs is the memory_write argument object.
type writeArgs struct {
	Path    string `json:"path"`
	Base    string `json:"base"`
	Content string `json:"content"`
}

// CallTool dispatches one tool call. A contract failure becomes an
// isError result so the model can read the code; only an unknown tool name
// is a protocol error.
func (s *Server) CallTool(ctx context.Context, name string, params json.RawMessage) (CallToolResult, *JSONRPCError) {
	switch name {
	case "memory_read":
		return s.memoryRead(ctx, params)
	case "memory_write":
		return s.memoryWrite(ctx, params)
	default:
		return CallToolResult{}, &JSONRPCError{
			Code:    codeInvalidParams,
			Message: "unknown tool " + name,
		}
	}
}

// memoryRead reads one file and returns the same fields as `read --json`.
func (s *Server) memoryRead(ctx context.Context, params json.RawMessage) (CallToolResult, *JSONRPCError) {
	var args readArgs
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return CallToolResult{}, &JSONRPCError{Code: codeInvalidParams, Message: "invalid arguments: " + err.Error()}
		}
	}
	path := args.Path
	if path == "" {
		path = DefaultEntryPoint
	}

	file, err := s.service.ReadFile(ctx, path)
	if err != nil {
		return textResult(errorText(err), true), nil
	}
	payload := struct {
		FilePath     string `json:"filePath"`
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}{
		FilePath:     file.RelPath,
		FileRevision: file.FileRevision,
		RawContent:   file.RawContent,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return textResult(errorText(err), true), nil
	}
	return textResult(string(encoded), false), nil
}

// memoryWrite applies one managed write. A busy lock is retried once; every
// other failure is reported immediately with its contract code.
func (s *Server) memoryWrite(ctx context.Context, params json.RawMessage) (CallToolResult, *JSONRPCError) {
	var args writeArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return CallToolResult{}, &JSONRPCError{Code: codeInvalidParams, Message: "invalid arguments: " + err.Error()}
	}
	if args.Path == "" {
		return textResult("INVALID_ARGUMENT: path is required", true), nil
	}
	if args.Base == "" {
		return textResult("INVALID_ARGUMENT: base is required (a fileRevision from memory_read, or absent)", true), nil
	}

	var result app.MutationResult
	var err error
	for attempt := 1; attempt <= writeAttempts; attempt++ {
		result, err = s.service.Apply(ctx, app.MutationRequest{
			Changes: []app.FileChange{{
				Path:             args.Path,
				ExpectedRevision: args.Base,
				Action:           app.ActionPut,
				RawContent:       args.Content,
			}},
			Agent:     s.session.Agent,
			SessionID: s.session.SessionID,
			Device:    s.session.Device,
		})
		if err == nil || !isBusy(err) || attempt == writeAttempts {
			break
		}
	}
	if err != nil {
		return textResult(errorText(err), true), nil
	}

	payload := struct {
		Transaction string `json:"transaction"`
		Commit      string `json:"commit"`
		Revision    string `json:"revision"`
		Sync        string `json:"sync"`
	}{
		Transaction: result.TransactionID,
		Commit:      result.Commit,
		Revision:    result.Revision,
		Sync:        result.SyncState,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return textResult(errorText(err), true), nil
	}
	return textResult(string(encoded), false), nil
}

// isBusy reports whether err carries the BUSY contract code, so the retry
// stays narrow and never hides another failure.
func isBusy(err error) bool {
	contractErr, ok := memerr.As(err)
	return ok && contractErr.Code == memerr.Busy
}

// ToolNames returns the tool names in the order Tools reports them. It exists
// for tests and for the install-time capability check.
func ToolNames() []string {
	names := []string{"memory_read", "memory_write"}
	sort.Strings(names)
	return names
}
