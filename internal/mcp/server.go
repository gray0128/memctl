package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// maxLineBytes bounds one incoming message. Memory files are capped at 2 MiB;
// the envelope plus JSON escaping leaves headroom above that.
const maxLineBytes = 8 << 20

// Serve reads newline-delimited JSON-RPC messages from r and writes responses
// to w until r ends or ctx is cancelled. Diagnostics go to errOut and never
// include memory text or tool arguments.
func Serve(ctx context.Context, server *Server, r io.Reader, w io.Writer, errOut io.Writer) error {
	reader := bufio.NewReaderSize(r, 64<<10)
	writer := bufio.NewWriter(w)
	defer writer.Flush()

	initialized := false
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		line, err := readLine(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// One oversized line is a parse failure of that message only; the
			// stream stays usable because readLine drained the rest of it.
			if err == errLineTooLong {
				_ = writeMessage(writer, rpcError(nil, codeParseError, err.Error()))
				_ = writer.Flush()
				continue
			}
			fmt.Fprintf(errOut, "memctl mcp: cannot read input: %v\n", err)
			return nil
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var request JSONRPCRequest
		if err := json.Unmarshal(line, &request); err != nil {
			_ = writeMessage(writer, rpcError(nil, codeParseError, "invalid JSON: "+err.Error()))
			_ = writer.Flush()
			continue
		}
		if request.Method == "" {
			_ = writeMessage(writer, rpcError(request.ID, codeInvalidRequest, "missing method"))
			_ = writer.Flush()
			continue
		}

		response, respond := server.handle(ctx, request, &initialized)
		if !respond {
			continue
		}
		if err := writeMessage(writer, response); err != nil {
			fmt.Fprintf(errOut, "memctl mcp: cannot write output: %v\n", err)
			return nil
		}
		if err := writer.Flush(); err != nil {
			fmt.Fprintf(errOut, "memctl mcp: cannot flush output: %v\n", err)
			return nil
		}
	}
}

// readLine reads one newline-terminated message without unbounded buffering.
// A message longer than maxLineBytes is drained and reported as a parse error
// by the caller, so one bad line cannot exhaust memory.
func readLine(reader *bufio.Reader) ([]byte, error) {
	var buffer []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(buffer)+len(chunk) > maxLineBytes {
			// Discard the remainder of the oversized line, then fail this
			// read so the caller reports one error instead of parsing a
			// truncated message.
			for err == bufio.ErrBufferFull {
				_, err = reader.ReadSlice('\n')
			}
			return nil, errLineTooLong
		}
		buffer = append(buffer, chunk...)
		if err == nil {
			return bytes.TrimRight(buffer, "\n"), nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF && len(buffer) > 0 {
			return bytes.TrimRight(buffer, "\n"), nil
		}
		return nil, err
	}
}

// errLineTooLong marks an oversized incoming message.
var errLineTooLong = fmt.Errorf("message exceeds %d bytes", maxLineBytes)

// handle dispatches one request. respond is false for notifications, which
// must not receive a response.
func (s *Server) handle(ctx context.Context, request JSONRPCRequest, initialized *bool) (JSONRPCResponse, bool) {
	isNotification := len(request.ID) == 0

	switch request.Method {
	case "initialize":
		version := initializeVersion(request.Params)
		result := map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    "memctl",
				"version": serverVersion,
			},
		}
		if isNotification {
			return JSONRPCResponse{}, false
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: result}, true

	case "notifications/initialized":
		*initialized = true
		return JSONRPCResponse{}, false

	case "ping":
		if isNotification {
			return JSONRPCResponse{}, false
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{}}, true

	case "tools/list":
		if isNotification {
			return JSONRPCResponse{}, false
		}
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  map[string]any{"tools": s.Tools()},
		}, true

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return rpcError(request.ID, codeInvalidParams, "invalid tools/call params: "+err.Error()), !isNotification
		}
		if params.Name == "" {
			return rpcError(request.ID, codeInvalidParams, "tools/call requires a tool name"), !isNotification
		}
		result, rpcErr := s.CallTool(ctx, params.Name, params.Arguments)
		if isNotification {
			return JSONRPCResponse{}, false
		}
		if rpcErr != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: rpcErr}, true
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: result}, true

	default:
		if isNotification {
			return JSONRPCResponse{}, false
		}
		return rpcError(request.ID, codeMethodNotFound, "unsupported method "+request.Method), true
	}
}

// initializeVersion extracts the client's requested protocol version. A
// missing or malformed params object accepts the newest supported revision.
func initializeVersion(params json.RawMessage) string {
	var payload struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &payload)
	}
	return negotiateVersion(payload.ProtocolVersion)
}

// serverVersion is filled by the CLI so tools/list reports the running
// binary version. It stays "dev" in tests.
var serverVersion = "dev"

// SetServerVersion records the binary version reported in initialize.
func SetServerVersion(version string) {
	if version != "" {
		serverVersion = version
	}
}
