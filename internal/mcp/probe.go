package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ProbeTimeout bounds one capability probe of a memctl binary. The probe is a
// single-process round trip that normally finishes in milliseconds, but it runs
// on a machine that may be busy compiling, so the budget is generous enough not
// to misreport a slow-but-working binary as unavailable.
const ProbeTimeout = 10 * time.Second

// ProbeResult describes what one memctl binary reports over the MCP channel.
type ProbeResult struct {
	// Path is the probed executable.
	Path string
	// Version is the serverInfo.version the binary reported, if any.
	Version string
	// Tools are the tool names tools/list returned, in server order.
	Tools []string
}

// HasMemoryTools reports whether both memory tools are available.
func (r ProbeResult) HasMemoryTools() bool {
	read, write := false, false
	for _, name := range r.Tools {
		switch name {
		case "memory_read":
			read = true
		case "memory_write":
			write = true
		}
	}
	return read && write
}

// Probe starts path as `mcp --agent agent`, completes the handshake and reads
// tools/list, then closes stdin so the child exits. It writes no file: the
// probe only speaks the protocol. A binary without the subcommand, or one that
// answers with a protocol error, returns an error.
func Probe(ctx context.Context, path, agent string) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "mcp", "--agent", agent)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ProbeResult{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ProbeResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return ProbeResult{}, err
	}

	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + ProtocolVersion + `"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"
	_, writeErr := stdin.Write([]byte(requests))
	// Closing stdin ends the server loop after it answers the buffered lines;
	// the child is reaped below on every path, so no process is left behind.
	_ = stdin.Close()
	if writeErr != nil {
		_ = cmd.Wait()
		return ProbeResult{}, writeErr
	}

	result := ProbeResult{Path: path}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var response JSONRPCResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			continue
		}
		if response.Error != nil {
			_ = cmd.Wait()
			return ProbeResult{}, fmt.Errorf("%d %s", response.Error.Code, response.Error.Message)
		}
		encoded, err := json.Marshal(response.Result)
		if err != nil {
			continue
		}
		var initResult struct {
			ServerInfo struct {
				Version string `json:"version"`
			} `json:"serverInfo"`
		}
		if json.Unmarshal(encoded, &initResult) == nil && initResult.ServerInfo.Version != "" {
			result.Version = initResult.ServerInfo.Version
		}
		var listResult struct {
			Tools []Tool `json:"tools"`
		}
		if json.Unmarshal(encoded, &listResult) == nil && listResult.Tools != nil {
			for _, tool := range listResult.Tools {
				result.Tools = append(result.Tools, tool.Name)
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return ProbeResult{}, err
	}
	if err := cmd.Wait(); err != nil {
		return ProbeResult{}, fmt.Errorf("exit status: %w", err)
	}
	if result.Tools == nil {
		return ProbeResult{}, fmt.Errorf("no tools/list response")
	}
	return result, nil
}
