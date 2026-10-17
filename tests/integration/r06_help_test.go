package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestR06AgentHelpI18n verifies scenario R06 from
// docs/05-validation-and-delivery.md. It builds the real binary and runs help
// with an isolated, nonexistent HOME/memory/state and an empty PATH:
//
//   - every documented command has a page, and `<command> --help` is equivalent
//   - help never creates the repository or state directory
//   - --json is valid, schema-versioned and covers every command
//   - --lang, MEMCTL_LANG and locale fallback select English or Chinese
//   - unknown topics and invalid languages are usage errors
func TestR06AgentHelpI18n(t *testing.T) {
	binPath := buildMemctlBinary(t)
	tmpDir := t.TempDir()
	isolatedHome := filepath.Join(tmpDir, "nonexistent-home")
	isolatedMem := filepath.Join(tmpDir, "nonexistent-memory")
	isolatedState := filepath.Join(tmpDir, "nonexistent-state")

	run := func(extraEnv []string, args ...string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Dir = tmpDir
		cmd.Env = append([]string{
			"HOME=" + isolatedHome,
			"AGENT_MEMORY_DIR=" + isolatedMem,
			"AGENT_MEMORY_STATE=" + isolatedState,
			"PATH=",
		}, extraEnv...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				t.Fatalf("unexpected execution error for %v: %v", args, err)
			}
		}
		return exitCode, stdout.String(), stderr.String()
	}

	code, stdout, stderr := run(nil, "help")
	if code != 0 {
		t.Fatalf("help exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	for _, want := range []string{"Usage:", "memctl <subcommand>", "Commands:", "AGENT_MEMORY_DIR", "memctl help --json"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help stdout lacks %q: %q", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("help stderr = %q, want empty", stderr)
	}

	commands := []string{
		"help", "version", "init", "read", "write", "sync", "end", "start",
		"context", "pull", "tag", "wrap", "resolve", "recover", "doctor",
		"dream", "install", "dsh", "pi", "update", "ui",
	}
	for _, command := range commands {
		for _, args := range [][]string{{"help", command}, {command, "--help"}} {
			code, stdout, stderr = run(nil, args...)
			if code != 0 {
				t.Fatalf("%v exit code = %d, want 0 (stderr=%q)", args, code, stderr)
			}
			if !strings.Contains(stdout, "memctl "+command+" - ") {
				t.Fatalf("%v stdout lacks the command page: %q", args, stdout)
			}
			if stderr != "" {
				t.Fatalf("%v stderr = %q, want empty", args, stderr)
			}
		}
	}

	code, stdout, stderr = run(nil, "help", "--json")
	if code != 0 {
		t.Fatalf("help --json exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	var full struct {
		SchemaVersion int    `json:"schemaVersion"`
		Tool          string `json:"tool"`
		Language      string `json:"language"`
		Commands      []struct {
			Name      string   `json:"name"`
			Usage     []string `json:"usage"`
			ExitCodes []struct {
				Code int `json:"code"`
			} `json:"exitCodes"`
			Examples []struct {
				Command string `json:"command"`
			} `json:"examples"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(stdout), &full); err != nil {
		t.Fatalf("help --json is not valid JSON: %v\n%s", err, stdout)
	}
	if full.SchemaVersion != 1 || full.Tool != "memctl" || full.Language != "en" {
		t.Fatalf("help --json envelope = %+v", full)
	}
	if len(full.Commands) != len(commands) {
		t.Fatalf("help --json has %d commands, want %d", len(full.Commands), len(commands))
	}
	for _, command := range full.Commands {
		if command.Name == "" || len(command.Usage) == 0 || len(command.ExitCodes) == 0 || len(command.Examples) == 0 {
			t.Fatalf("help --json command is incomplete: %+v", command)
		}
	}

	code, stdout, stderr = run(nil, "help", "write", "--json")
	if code != 0 {
		t.Fatalf("help write --json exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	var single struct {
		Language string `json:"language"`
		Commands []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(stdout), &single); err != nil {
		t.Fatalf("help write --json is not valid JSON: %v\n%s", err, stdout)
	}
	if single.Language != "en" || len(single.Commands) != 1 || single.Commands[0].Name != "write" {
		t.Fatalf("help write --json = %+v", single)
	}

	code, stdout, stderr = run([]string{"MEMCTL_LANG=zh"}, "help", "read")
	if code != 0 {
		t.Fatalf("Chinese help exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	for _, want := range []string{"用法:", "说明:", "退出码:", "示例:"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("Chinese help lacks %q: %q", want, stdout)
		}
	}

	code, stdout, stderr = run([]string{"MEMCTL_LANG=en"}, "help", "--lang", "zh", "read")
	if code != 0 {
		t.Fatalf("--lang zh exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "用法:") {
		t.Fatalf("--lang zh did not override MEMCTL_LANG: %q", stdout)
	}

	code, stdout, stderr = run([]string{"MEMCTL_LANG=zh"}, "help", "--json")
	if code != 0 {
		t.Fatalf("Chinese help --json exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, `"language":"zh"`) {
		t.Fatalf("Chinese help --json language is not zh: %q", stdout)
	}

	code, stdout, stderr = run(nil, "help", "nonexistent")
	if code != 2 {
		t.Fatalf("unknown help topic exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("unknown help topic stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("unknown help topic stderr = %q", stderr)
	}

	code, stdout, stderr = run(nil, "help", "--lang", "fr")
	if code != 2 {
		t.Fatalf("invalid --lang exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("invalid --lang stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "invalid --lang") {
		t.Fatalf("invalid --lang stderr = %q", stderr)
	}

	for _, path := range []string{isolatedMem, isolatedState} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("help created %s, stat err = %v", path, err)
		}
	}
}
