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
	// ListEntries backs memory_entries. It is the same read-only query the Web
	// UI uses, so the tool cannot widen the repository boundary.
	ListEntries(ctx context.Context, query app.EntryQuery) (app.EntryPage, error)
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
			Name:        "memory_entries",
			Description: "List or filter memory entries without reading whole files. Matching is a case-insensitive substring over the entry fact text; filters narrow by file, agent, device and added date. Use this to find where a fact lives before calling memory_read. Read-only; it never writes.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q": map[string]any{
						"type":        "string",
						"description": "Case-insensitive substring to match against the entry fact text.",
					},
					"file": map[string]any{
						"type":        "string",
						"description": "Only entries of this repository-relative file.",
					},
					"agent": map[string]any{
						"type":        "string",
						"description": "Only entries whose source records this agent name.",
					},
					"device": map[string]any{
						"type":        "string",
						"description": "Only entries whose source records this device label.",
					},
					"from": map[string]any{
						"type":        "string",
						"description": "Earliest added date (YYYY-MM-DD, inclusive).",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "Latest added date (YYYY-MM-DD, inclusive).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Page size, 1-200. Defaults to 50.",
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Number of matching entries to skip.",
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

// entriesArgs is the memory_entries argument object.
type entriesArgs struct {
	Q      string `json:"q"`
	File   string `json:"file"`
	Agent  string `json:"agent"`
	Device string `json:"device"`
	From   string `json:"from"`
	To     string `json:"to"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
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
	case "memory_entries":
		return s.memoryEntries(ctx, params)
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
		FilePath      string `json:"filePath"`
		FileRevision  string `json:"fileRevision"`
		RawContent    string `json:"rawContent"`
		HiddenExpired int    `json:"hiddenExpired,omitempty"`
	}{
		FilePath:      file.RelPath,
		FileRevision:  file.FileRevision,
		RawContent:    file.RawContent,
		HiddenExpired: file.HiddenExpired,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return textResult(errorText(err), true), nil
	}
	return textResult(string(encoded), false), nil
}

// memoryEntries lists one page of entries. It returns the same shape the Web UI
// reads from GET /api/v1/entries, so an agent and a human see identical results.
func (s *Server) memoryEntries(ctx context.Context, params json.RawMessage) (CallToolResult, *JSONRPCError) {
	var args entriesArgs
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return CallToolResult{}, &JSONRPCError{Code: codeInvalidParams, Message: "invalid arguments: " + err.Error()}
		}
	}
	if args.Limit < 0 || args.Offset < 0 {
		return textResult("INVALID_ARGUMENT: limit and offset must not be negative", true), nil
	}
	page, err := s.service.ListEntries(ctx, app.EntryQuery{
		Q:      args.Q,
		Agent:  args.Agent,
		Device: args.Device,
		File:   args.File,
		From:   args.From,
		To:     args.To,
		Limit:  args.Limit,
		Offset: args.Offset,
	})
	if err != nil {
		return textResult(errorText(err), true), nil
	}
	encoded, err := json.Marshal(page)
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
	names := []string{"memory_entries", "memory_read", "memory_write"}
	sort.Strings(names)
	return names
}
