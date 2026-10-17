package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpLanguagePriority(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		args     []string
		wantLang helpLang
		wantArgs []string
		wantErr  bool
	}{
		{
			name:     "default is English",
			args:     []string{"help"},
			wantLang: helpLangEN,
			wantArgs: []string{"help"},
		},
		{
			name:     "MEMCTL_LANG selects Chinese",
			env:      map[string]string{"MEMCTL_LANG": "zh"},
			args:     []string{"help"},
			wantLang: helpLangZH,
			wantArgs: []string{"help"},
		},
		{
			name:     "MEMCTL_LANG wins over locale",
			env:      map[string]string{"MEMCTL_LANG": "en-US", "LC_ALL": "zh_CN.UTF-8"},
			args:     []string{"help"},
			wantLang: helpLangEN,
			wantArgs: []string{"help"},
		},
		{
			name:     "LC_ALL selects Chinese",
			env:      map[string]string{"LC_ALL": "zh_CN.UTF-8"},
			args:     []string{"help"},
			wantLang: helpLangZH,
			wantArgs: []string{"help"},
		},
		{
			name:     "LC_MESSAGES is the next source",
			env:      map[string]string{"LC_MESSAGES": "zh"},
			args:     []string{"help"},
			wantLang: helpLangZH,
			wantArgs: []string{"help"},
		},
		{
			name:     "LANG is the final locale source",
			env:      map[string]string{"LANG": "zh_CN.UTF-8"},
			args:     []string{"help"},
			wantLang: helpLangZH,
			wantArgs: []string{"help"},
		},
		{
			name:     "invalid MEMCTL_LANG falls through to the locale",
			env:      map[string]string{"MEMCTL_LANG": "fr", "LANG": "zh_CN.UTF-8"},
			args:     []string{"help"},
			wantLang: helpLangZH,
			wantArgs: []string{"help"},
		},
		{
			name:     "LC_ALL=C overrides a lower-priority Chinese LANG",
			env:      map[string]string{"LC_ALL": "C", "LANG": "zh_CN.UTF-8"},
			args:     []string{"help"},
			wantLang: helpLangEN,
			wantArgs: []string{"help"},
		},
		{
			name:     "unsupported locale selects English",
			env:      map[string]string{"LC_ALL": "fr_FR.UTF-8", "LANG": "zh_CN.UTF-8"},
			args:     []string{"help"},
			wantLang: helpLangEN,
			wantArgs: []string{"help"},
		},
		{
			name:     "--lang overrides the environment and is removed",
			env:      map[string]string{"MEMCTL_LANG": "en"},
			args:     []string{"help", "--lang", "zh"},
			wantLang: helpLangZH,
			wantArgs: []string{"help"},
		},
		{
			name:     "--lang=value is accepted",
			env:      map[string]string{"MEMCTL_LANG": "zh"},
			args:     []string{"help", "--lang=en"},
			wantLang: helpLangEN,
			wantArgs: []string{"help"},
		},
		{
			name:     "--lang after the separator is not consumed",
			env:      map[string]string{"MEMCTL_LANG": "en"},
			args:     []string{"wrap", "--", "--lang", "zh"},
			wantLang: helpLangEN,
			wantArgs: []string{"wrap", "--", "--lang", "zh"},
		},
		{
			name:    "--lang without a value is an error",
			args:    []string{"help", "--lang"},
			wantErr: true,
		},
		{
			name:    "invalid --lang is an error",
			args:    []string{"help", "--lang", "fr"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string { return tc.env[key] }
			lang, args, err := resolveHelpLanguage(getenv, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got language %q and args %v", lang, args)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lang != tc.wantLang {
				t.Fatalf("language = %q, want %q", lang, tc.wantLang)
			}
			if strings.Join(args, "\x00") != strings.Join(tc.wantArgs, "\x00") {
				t.Fatalf("args = %v, want %v", args, tc.wantArgs)
			}
		})
	}
}

func TestHelpRequestStopsAtSeparator(t *testing.T) {
	if wants, _ := helpRequest([]string{"write", "--help"}); !wants {
		t.Fatal("--help before the separator must request help")
	}
	if wants, jsonMode := helpRequest([]string{"write", "--help", "--json"}); !wants || !jsonMode {
		t.Fatalf("helpRequest = (%v, %v), want (true, true)", wants, jsonMode)
	}
	if wants, _ := helpRequest([]string{"wrap", "--", "--help"}); wants {
		t.Fatal("--help after the separator belongs to the wrapped command")
	}
}

func TestHelpCatalogIsComplete(t *testing.T) {
	names := make(map[string]bool)
	for _, command := range helpCommandData {
		if command.Name == "" {
			t.Fatal("a help command has an empty name")
		}
		if names[command.Name] {
			t.Fatalf("duplicate help command %q", command.Name)
		}
		names[command.Name] = true

		if command.Summary.EN == "" || command.Summary.ZH == "" {
			t.Fatalf("%s: summary must be localized", command.Name)
		}
		if command.Description.EN == "" || command.Description.ZH == "" {
			t.Fatalf("%s: description must be localized", command.Name)
		}
		if len(command.Usage) == 0 {
			t.Fatalf("%s: usage is empty", command.Name)
		}
		if len(command.ExitCodes) == 0 {
			t.Fatalf("%s: exit codes are missing", command.Name)
		}
		if len(command.Examples) == 0 {
			t.Fatalf("%s: examples are missing", command.Name)
		}
		for _, related := range command.Related {
			if _, ok := lookupHelpCommand(related); !ok {
				t.Fatalf("%s: unknown related command %q", command.Name, related)
			}
		}
	}

	grouped := make(map[string]int)
	for _, group := range helpRootData.Groups {
		if group.Title.EN == "" || group.Title.ZH == "" {
			t.Fatal("a help group title must be localized")
		}
		for _, name := range group.Names {
			if _, ok := lookupHelpCommand(name); !ok {
				t.Fatalf("group %q lists unknown command %q", group.Title.EN, name)
			}
			grouped[name]++
		}
	}
	for name := range names {
		if grouped[name] != 1 {
			t.Fatalf("%s appears %d times in help groups, want exactly 1", name, grouped[name])
		}
	}
	if len(grouped) != len(names) {
		t.Fatalf("groups cover %d commands, catalog has %d", len(grouped), len(names))
	}
}

func TestHelpPagesCoverEveryCommand(t *testing.T) {
	t.Setenv("MEMCTL_LANG", "en")
	for _, name := range helpCommandNames() {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"help", name}, strings.NewReader(""), &stdout, &stderr, "v-test")
			if code != ExitOK {
				t.Fatalf("help %s exit code = %d, want 0 (stderr=%q)", name, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "memctl "+name+" - ") {
				t.Fatalf("help %s stdout lacks the command title: %q", name, stdout.String())
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Fatalf("help %s stdout lacks Usage:", name)
			}
			if !strings.Contains(stdout.String(), "Exit codes:") {
				t.Fatalf("help %s stdout lacks Exit codes:", name)
			}

			stdout.Reset()
			stderr.Reset()
			code = Run([]string{name, "--help"}, strings.NewReader(""), &stdout, &stderr, "v-test")
			if code != ExitOK {
				t.Fatalf("%s --help exit code = %d, want 0 (stderr=%q)", name, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "memctl "+name+" - ") {
				t.Fatalf("%s --help stdout lacks the command title: %q", name, stdout.String())
			}
		})
	}
}

func TestHelpTextIsLocalized(t *testing.T) {
	t.Setenv("MEMCTL_LANG", "en")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"help"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("English help exit code = %d (stderr=%q)", code, stderr.String())
	}
	for _, want := range []string{"Usage:", "Commands:", "AGENT_MEMORY_DIR", "MEMCTL_LANG", "memctl write"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("English help lacks %q", want)
		}
	}
	if strings.Contains(stdout.String(), "用法:") {
		t.Fatal("English help contains Chinese labels")
	}

	stdout.Reset()
	stderr.Reset()
	t.Setenv("MEMCTL_LANG", "zh")
	if code := Run([]string{"help"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("Chinese help exit code = %d (stderr=%q)", code, stderr.String())
	}
	for _, want := range []string{"用法:", "命令:", "AGENT_MEMORY_DIR", "MEMCTL_LANG", "memctl write"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("Chinese help lacks %q", want)
		}
	}
	if strings.Contains(stdout.String(), "Usage:") {
		t.Fatal("Chinese help contains English labels")
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"help", "write", "--lang", "zh"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("Chinese command help exit code = %d (stderr=%q)", code, stderr.String())
	}
	for _, want := range []string{"说明:", "退出码:", "示例:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("Chinese write help lacks %q", want)
		}
	}
}

func TestHelpJSON(t *testing.T) {
	t.Setenv("MEMCTL_LANG", "en")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"help", "--json"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("help --json exit code = %d (stderr=%q)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("help --json stderr = %q, want empty", stderr.String())
	}

	var payload helpJSON
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("help --json is not valid JSON: %v\n%s", err, stdout.String())
	}
	if decoder.More() {
		t.Fatal("help --json emitted more than one JSON value")
	}
	if payload.SchemaVersion != helpSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", payload.SchemaVersion, helpSchemaVersion)
	}
	if payload.Tool != "memctl" || payload.Language != "en" {
		t.Fatalf("unexpected tool/language: %q/%q", payload.Tool, payload.Language)
	}
	if len(payload.QuickStart) == 0 || len(payload.Rules) == 0 || len(payload.Environment) == 0 {
		t.Fatal("root help JSON lacks quickStart, rules or environment")
	}
	if len(payload.Commands) != len(helpCommandData) {
		t.Fatalf("help JSON has %d commands, catalog has %d", len(payload.Commands), len(helpCommandData))
	}
	for _, command := range payload.Commands {
		if command.Name == "" || command.Summary == "" || len(command.Usage) == 0 || len(command.ExitCodes) == 0 || len(command.Examples) == 0 {
			t.Fatalf("incomplete command JSON: %+v", command)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"help", "write", "--json"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("help write --json exit code = %d (stderr=%q)", code, stderr.String())
	}
	payload = helpJSON{}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("help write --json is not valid JSON: %v", err)
	}
	if len(payload.Commands) != 1 || payload.Commands[0].Name != "write" {
		t.Fatalf("help write --json commands = %+v", payload.Commands)
	}
	if payload.Title != "" || len(payload.QuickStart) != 0 {
		t.Fatal("single-command JSON must not include the root overview")
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"write", "--help", "--json"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("write --help --json exit code = %d (stderr=%q)", code, stderr.String())
	}
	payload = helpJSON{}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("write --help --json is not valid JSON: %v", err)
	}
	if len(payload.Commands) != 1 || payload.Commands[0].Name != "write" {
		t.Fatalf("write --help --json commands = %+v", payload.Commands)
	}

	stdout.Reset()
	stderr.Reset()
	t.Setenv("MEMCTL_LANG", "zh")
	if code := Run([]string{"help", "read", "--json"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("Chinese help read --json exit code = %d (stderr=%q)", code, stderr.String())
	}
	payload = helpJSON{}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Chinese help read --json is not valid JSON: %v", err)
	}
	if payload.Language != "zh" {
		t.Fatalf("Chinese help JSON language = %q", payload.Language)
	}
	if len(payload.Commands) != 1 || !strings.Contains(payload.Commands[0].Summary, "读取") {
		t.Fatalf("Chinese help JSON is not localized: %+v", payload.Commands)
	}
}

func TestHelpErrors(t *testing.T) {
	t.Setenv("MEMCTL_LANG", "en")
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "unknown topic", args: []string{"help", "nonexistent"}, wantStderr: "unknown command"},
		{name: "extra topic", args: []string{"help", "read", "extra"}, wantStderr: "unexpected argument"},
		{name: "unknown help flag", args: []string{"help", "--unknown"}, wantStderr: "unknown flag"},
		{name: "missing lang value", args: []string{"help", "--lang"}, wantStderr: "--lang requires a value"},
		{name: "invalid lang value", args: []string{"help", "--lang", "fr"}, wantStderr: "invalid --lang"},
		{name: "unknown command with help flag", args: []string{"nonexistent", "--help"}, wantStderr: "unknown command"},
		{name: "usage error points at the command help", args: []string{"write"}, wantStderr: `Run "memctl help write" for usage.`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, strings.NewReader(""), &stdout, &stderr, "v-test")
			if code != ExitUsageError {
				t.Fatalf("exit code = %d, want %d (stderr=%q)", code, ExitUsageError, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

func TestHelpDoesNotTouchRepository(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "missing-memory")
	state := filepath.Join(home, "missing-state")
	t.Setenv("HOME", home)
	t.Setenv("AGENT_MEMORY_DIR", repo)
	t.Setenv("AGENT_MEMORY_STATE", state)
	t.Setenv("MEMCTL_LANG", "en")

	commands := [][]string{
		{"help"},
		{"help", "write"},
		{"help", "--json"},
		{"help", "write", "--json"},
		{"write", "--help"},
		{"write", "--help", "--json"},
	}
	for _, args := range commands {
		var stdout, stderr bytes.Buffer
		if code := Run(args, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
			t.Fatalf("%v exit code = %d, want 0 (stderr=%q)", args, code, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("%v stderr = %q, want empty", args, stderr.String())
		}
	}
	for _, path := range []string{repo, state} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("help created %s, stat err = %v", path, err)
		}
	}
}
