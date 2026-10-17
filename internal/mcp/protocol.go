// Package mcp implements the Model Context Protocol server half of memctl.
//
// The server speaks newline-delimited JSON-RPC 2.0 on stdin and stdout, so a
// host's MCP client can start it as a child process. It exposes two tools that
// map onto the existing application service: memory_read reads one file with
// its revision baseline and memory_write applies one managed write. Protocol
// handling, path scoping and error mapping live here; storage, locking, Git
// commits and background synchronization stay in internal/app. See
// docs/09-mcp-memory-tools.md.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gray0128/memctl/internal/memerr"
)

// ProtocolVersion is the newest MCP revision this server implements.
const ProtocolVersion = "2025-06-18"

// supportedVersions lists the revisions the server can answer when the client
// requests an older one. The newest entry is used when the client omits a
// version.
var supportedVersions = []string{ProtocolVersion, "2025-03-26", "2024-11-05"}

// JSON-RPC error codes from the MCP specification.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// JSONRPCRequest is one incoming call or notification.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCError is the error member of a failed response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSONRPCResponse is one outgoing response. Result and Error are mutually
// exclusive; ID is omitted for notifications, which never carry a response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// Tool describes one tool in the tools/list result.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// TextContent is one MCP content block.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallToolResult is the tools/call result. IsError marks a tool-level failure
// that the model should read and act on, as opposed to a protocol error.
type CallToolResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// textResult builds a single-text-block tool result.
func textResult(text string, isError bool) CallToolResult {
	return CallToolResult{Content: []TextContent{{Type: "text", Text: text}}, IsError: isError}
}

// errorText renders a contract error the same way the CLI does, so a model can
// match the tool output against CLI help and documentation.
func errorText(err error) string {
	if contractErr, ok := memerr.As(err); ok {
		return fmt.Sprintf("%s: %s", contractErr.Code, contractErr.Message)
	}
	return err.Error()
}

// rpcError builds a protocol-level error response.
func rpcError(id json.RawMessage, code int, message string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
}

// writeMessage encodes one response as a single line. The encoder disables
// HTML escaping so memory text round-trips unchanged.
func writeMessage(w io.Writer, message JSONRPCResponse) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(message)
}

// negotiateVersion returns the protocol revision to answer with. An empty
// request accepts the newest revision; an unknown revision falls back to the
// newest supported one, which the client may reject.
func negotiateVersion(requested string) string {
	if requested == "" {
		return supportedVersions[0]
	}
	for _, version := range supportedVersions {
		if version == requested {
			return version
		}
	}
	return supportedVersions[0]
}

// ScrubError removes newlines so a diagnostic stays on one line of stderr.
func ScrubError(err error) string {
	return errorText(err)
}
