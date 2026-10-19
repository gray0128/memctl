package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This file verifies T071: the `memctl mcp` subcommand and its two tools. It
// drives the real binary over stdin/stdout against a temporary memory
// repository, so the protocol, the managed write transaction and the error
// mapping are exercised together. See docs/09-mcp-memory-tools.md.

// mcpResponse is one decoded JSON-RPC response line.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callToolResult is the decoded tools/call result.
type callToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// runMCPSession feeds request lines to one `memctl mcp` process and returns the
// decoded responses plus stderr.
func runMCPSession(t *testing.T, bin, dir string, env []string, lines ...string) ([]mcpResponse, string) {
	t.Helper()
	cmd := exec.Command(bin, "mcp", "--agent", "test-agent", "--sid", "test-session")
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("memctl mcp: %v\nstderr=%s", err, stderr.String())
	}

	var responses []mcpResponse
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var response mcpResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout line is not JSON-RPC: %v\n%s", err, line)
		}
		responses = append(responses, response)
	}
	return responses, stderr.String()
}

func decodeCallResult(t *testing.T, response mcpResponse) callToolResult {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("protocol error: %d %s", response.Error.Code, response.Error.Message)
	}
	var result callToolResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("tools/call result: %v", err)
	}
	return result
}

// TestM01MCPHandshake proves the handshake and the tool list through the real
// binary.
func TestM01MCPHandshake(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")

	responses, stderr := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	if len(responses) != 2 {
		t.Fatalf("want 2 responses, got %d", len(responses))
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(responses[0].Result, &initResult); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	if initResult.ProtocolVersion != "2025-06-18" || initResult.ServerInfo.Name != "memctl" {
		t.Fatalf("unexpected initialize result %+v", initResult)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(responses[1].Result, &list); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(list.Tools) != 3 || list.Tools[0].Name != "memory_read" ||
		list.Tools[1].Name != "memory_entries" || list.Tools[2].Name != "memory_write" {
		t.Fatalf("unexpected tool list %+v", list.Tools)
	}
}

// TestM03MCPReadMatchesCLI proves memory_read returns the same content and
// revision as `read --json`.
func TestM03MCPReadMatchesCLI(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")

	cliRead := runMemctl(t, bin, home, env, "read", "MEMORY.md", "--json")
	if cliRead.code != 0 {
		t.Fatalf("read --json: code=%d stderr=%s", cliRead.code, cliRead.stderr)
	}
	var cliPayload struct {
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}
	if err := json.Unmarshal([]byte(cliRead.stdout), &cliPayload); err != nil {
		t.Fatalf("cli read payload: %v", err)
	}

	responses, _ := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read","arguments":{"path":"MEMORY.md"}}}`,
	)
	result := decodeCallResult(t, responses[0])
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	var payload struct {
		FilePath     string `json:"filePath"`
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("tool payload: %v", err)
	}
	if payload.FileRevision != cliPayload.FileRevision || payload.RawContent != cliPayload.RawContent {
		t.Fatalf("MCP and CLI reads differ:\n%+v\n%+v", payload, cliPayload)
	}
}

// TestM05MCPWriteCommits proves memory_write produces one local commit through
// the managed transaction and reports the same fields as `write`.
func TestM05MCPWriteCommits(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")

	// A path whose parent directory does not exist is refused instead of
	// silently creating directories, matching the CLI contract.
	responses, _ := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write","arguments":{"path":"notes/mcp-topic.md","base":"absent","content":"# MCP topic\n"}}}`,
	)
	result := decodeCallResult(t, responses[0])
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "NOT_FOUND: ") {
		t.Fatalf("want NOT_FOUND for a missing parent, got %q", result.Content[0].Text)
	}

	// The entry file must commit and return the same fields as the CLI write.
	readResponses, _ := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read","arguments":{}}}`,
	)
	var readPayload struct {
		FileRevision string `json:"fileRevision"`
	}
	readResult := decodeCallResult(t, readResponses[0])
	if err := json.Unmarshal([]byte(readResult.Content[0].Text), &readPayload); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	writeLine := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write","arguments":{"path":"MEMORY.md","base":"` +
		readPayload.FileRevision + `","content":"# Memory\n\n## Index\n\n- mcp-written\n"}}}`
	writeResponses, _ := runMCPSession(t, bin, home, env, writeLine)
	writeResult := decodeCallResult(t, writeResponses[0])
	if writeResult.IsError {
		t.Fatalf("unexpected write error: %s", writeResult.Content[0].Text)
	}
	var payload struct {
		Transaction string `json:"transaction"`
		Commit      string `json:"commit"`
		Revision    string `json:"revision"`
		Sync        string `json:"sync"`
	}
	if err := json.Unmarshal([]byte(writeResult.Content[0].Text), &payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if payload.Transaction == "" || payload.Commit == "" || payload.Revision == "" || payload.Sync == "" {
		t.Fatalf("incomplete write result %+v", payload)
	}

	after := runMemctl(t, bin, home, env, "read", "MEMORY.md", "--json")
	if after.code != 0 {
		t.Fatalf("read after write: %s", after.stderr)
	}
	if !strings.Contains(after.stdout, "mcp-written") {
		t.Fatalf("write did not reach the repository: %s", after.stdout)
	}
}

// TestM06MCPStaleBaseline proves a stale base is rejected without a commit.
func TestM06MCPStaleBaseline(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	headBefore := runMemctl(t, bin, repo, env, "read", "MEMORY.md", "--json")
	if headBefore.code != 0 {
		t.Fatalf("read: %s", headBefore.stderr)
	}

	responses, _ := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write","arguments":{"path":"MEMORY.md","base":"sha256:deadbeef","content":"stale"}}}`,
	)
	result := decodeCallResult(t, responses[0])
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "REVISION_MISMATCH: ") {
		t.Fatalf("unexpected result %q", result.Content[0].Text)
	}

	after := runMemctl(t, bin, home, env, "read", "MEMORY.md", "--json")
	if after.code != 0 || strings.Contains(after.stdout, "stale") {
		t.Fatalf("stale write reached the repository: %s", after.stdout)
	}
}

// TestM04MCPPathEscape proves a path outside the repository is refused with
// the contract code and no file is created.
func TestM04MCPPathEscape(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")

	outside := filepath.Join(root, "outside.md")
	responses, _ := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read","arguments":{"path":"../../outside.md"}}}`,
	)
	result := decodeCallResult(t, responses[0])
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "INVALID_PATH: ") {
		t.Fatalf("unexpected result %q", result.Content[0].Text)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a file outside the repository was touched")
	}
}

// TestM07MCPNoRepo proves a missing repository is reported as NO_REPO instead
// of creating one.
func TestM07MCPNoRepo(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	env := a4Env(home, repo, "AGENT_MEMORY_DEVICE=test-device", "AGENT_MEMORY_AGENT=test-agent")

	responses, _ := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read","arguments":{}}}`,
	)
	result := decodeCallResult(t, responses[0])
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "NO_REPO: ") {
		t.Fatalf("unexpected result %q", result.Content[0].Text)
	}
	if _, err := os.Stat(repo); err == nil {
		t.Fatal("mcp created a repository instead of reporting NO_REPO")
	}
}

// TestM01MCPUsageErrors proves unknown arguments are usage errors.
func TestM01MCPUsageErrors(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")

	result := runMemctl(t, bin, home, env, "mcp", "--bogus")
	if result.code != 2 {
		t.Fatalf("want exit 2, got %d (stderr=%s)", result.code, result.stderr)
	}
	result = runMemctl(t, bin, home, env, "mcp", "--agent")
	if result.code != 2 {
		t.Fatalf("want exit 2 for a missing value, got %d", result.code)
	}
}

// TestM13MCPEntriesFindsFactThenReads proves the read path an agent actually
// needs behind a host sandbox: locate a fact with memory_entries, then read the
// file it names with memory_read. It also proves the tool is read-only and that
// the CLI returns the same match.
func TestM13MCPEntriesFindsFactThenReads(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	topic := filepath.Join(repo, "notes", "prefs.md")
	if err := os.MkdirAll(filepath.Dir(topic), 0o755); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	if err := os.WriteFile(topic, []byte(
		"# Preferences\n\n- 编辑器偏好是 Vim [source: test; added: 2026-01-05]\n"+
			"- 终端使用 Ghostty [source: test; added: 2026-06-01]\n"), 0o644); err != nil {
		t.Fatalf("write topic: %v", err)
	}
	if out, err := gitCommandAt(repo, "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := gitCommandAt(repo, "commit", "-q", "-m", "seed topic"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	before := gitOutput(t, env, repo, "rev-parse", "HEAD")

	responses, stderr := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"memory_entries","arguments":{"q":"vim"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_read","arguments":{"path":"notes/prefs.md"}}}`,
	)
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	if len(responses) != 3 {
		t.Fatalf("want 3 responses, got %d", len(responses))
	}

	entries := decodeCallResult(t, responses[1])
	if entries.IsError {
		t.Fatalf("memory_entries failed: %+v", entries)
	}
	var page struct {
		Revision string `json:"revision"`
		Total    int    `json:"total"`
		Items    []struct {
			FilePath string `json:"filePath"`
			FactText string `json:"factText"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(entries.Content[0].Text), &page); err != nil {
		t.Fatalf("decode entry page: %v\n%s", err, entries.Content[0].Text)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("entry page = %+v, want the single Vim entry", page)
	}
	if page.Items[0].FilePath != "notes/prefs.md" || !strings.Contains(page.Items[0].FactText, "Vim") {
		t.Fatalf("entry page returned %+v", page.Items[0])
	}

	// The path the query returned is directly readable.
	read := decodeCallResult(t, responses[2])
	if read.IsError {
		t.Fatalf("memory_read failed: %+v", read)
	}
	var file struct {
		FilePath     string `json:"filePath"`
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}
	if err := json.Unmarshal([]byte(read.Content[0].Text), &file); err != nil {
		t.Fatalf("decode file: %v", err)
	}
	if file.FilePath != page.Items[0].FilePath || !strings.Contains(file.RawContent, "Ghostty") {
		t.Fatalf("read returned %+v", file)
	}

	// Read-only: neither the query nor the read changed the repository.
	if after := gitOutput(t, env, repo, "rev-parse", "HEAD"); after != before {
		t.Fatalf("a read path created a commit: %s -> %s", before, after)
	}
	if status := gitOutput(t, env, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("a read path changed the worktree: %s", status)
	}

	// The CLI reports the same match, so scripts and agents see one answer.
	cli := runMemctl(t, bin, home, env, "entries", "--q", "vim", "--json")
	if cli.code != 0 {
		t.Fatalf("entries: code=%d stderr=%q", cli.code, cli.stderr)
	}
	var cliPage struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(cli.stdout), &cliPage); err != nil {
		t.Fatalf("decode CLI page: %v\n%s", err, cli.stdout)
	}
	if cliPage.Total != page.Total {
		t.Fatalf("CLI total = %d, MCP total = %d", cliPage.Total, page.Total)
	}
}
