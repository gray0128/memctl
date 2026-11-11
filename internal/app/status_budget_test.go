package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// MaxStatusChars is the hard upper bound on the UTF-8 character count (Unicode
// code points after normalizing CRLF to LF) of docs/STATUS.md. Keeping the hot
// status ledger bounded prevents cross-round context bloat.
const MaxStatusChars = 12000

func checkStatusCharBudget(raw []byte, maxChars int) (int, error) {
	if !utf8.Valid(raw) {
		return 0, fmt.Errorf("docs/STATUS.md is not valid UTF-8")
	}
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	count := utf8.RuneCountInString(normalized)
	if count > maxChars {
		return count, fmt.Errorf(
			"docs/STATUS.md exceeds maximum character budget: got %d UTF-8 chars, max allowed is %d (over by %d).\n"+
				"To fix this:\n"+
				"  1. Archive older task completion records to docs/validation/stage-<x>.md and keep only the stage index + active/recent tasks under ## 任务.\n"+
				"  2. Remove completed or superseded items from ## 近期待办与下一步.\n"+
				"  3. Condense ## 当前事实 to current active facts instead of per-release or per-task changelogs.",
			count, maxChars, count-maxChars,
		)
	}
	return count, nil
}

func TestStatusMarkdownCharBudget(t *testing.T) {
	root := moduleRoot(t)
	statusPath := filepath.Join(root, "docs", "STATUS.md")
	raw, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("read %s: %v", statusPath, err)
	}
	count, err := checkStatusCharBudget(raw, MaxStatusChars)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("docs/STATUS.md UTF-8 character count: %d / %d", count, MaxStatusChars)
}

func TestCheckStatusCharBudgetBoundary(t *testing.T) {
	cases := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
		errSubstr []string
	}{
		{
			name:      "under_limit_11999_runes",
			input:     []byte(strings.Repeat("记", MaxStatusChars-1)),
			wantCount: MaxStatusChars - 1,
			wantErr:   false,
		},
		{
			name:      "exact_limit_12000_runes",
			input:     []byte(strings.Repeat("记", MaxStatusChars)),
			wantCount: MaxStatusChars,
			wantErr:   false,
		},
		{
			name:      "crlf_normalized_to_exact_limit_12000_runes",
			input:     []byte(strings.Repeat("a\r\n", MaxStatusChars/2)),
			wantCount: MaxStatusChars,
			wantErr:   false,
		},
		{
			name:      "over_limit_12001_runes_rejected",
			input:     []byte(strings.Repeat("记", MaxStatusChars+1)),
			wantCount: MaxStatusChars + 1,
			wantErr:   true,
			errSubstr: []string{
				"12001",
				"12000",
				"docs/validation/stage-",
				"近期待办与下一步",
				"当前事实",
			},
		},
		{
			name:    "invalid_utf8_rejected",
			input:   []byte{0xff, 0xfe, 0xfd},
			wantErr: true,
			errSubstr: []string{
				"not valid UTF-8",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotCount, err := checkStatusCharBudget(tc.input, MaxStatusChars)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (count=%d)", gotCount)
				}
				for _, sub := range tc.errSubstr {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("error %q missing expected substring %q", err.Error(), sub)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotCount != tc.wantCount {
				t.Fatalf("count = %d, want %d", gotCount, tc.wantCount)
			}
		})
	}
}

func TestNextRoundScriptEnforcesStatusBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash script gate test skipped on windows")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	root := moduleRoot(t)
	scriptPath := filepath.Join(root, "scripts", "next-round.sh")
	tmpDir := t.TempDir()

	promptPath := filepath.Join(tmpDir, "next-round.md")
	promptContent := strings.Join([]string{
		"## 有效规则与边界",
		"遵守项目规范。",
		"## 本轮结果与证据",
		"测试全部通过。",
		"## 剩余事项与依赖",
		"无阻塞项。",
		"## 下一轮目标",
		"执行下一轮验证。",
	}, "\n")
	if err := os.WriteFile(promptPath, []byte(promptContent), 0o600); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	okStatusPath := filepath.Join(tmpDir, "STATUS-12000.md")
	if err := os.WriteFile(okStatusPath, []byte(strings.Repeat("验", MaxStatusChars)), 0o600); err != nil {
		t.Fatalf("write ok status: %v", err)
	}
	cmdOK := exec.Command("bash", scriptPath, "--check-only", "--dry-run", "--prompt-file", promptPath)
	cmdOK.Dir = root
	cmdOK.Env = append(os.Environ(),
		"MEMCTL_STATUS_FILE="+okStatusPath,
		"ORCA_CLI_COMMAND=/nonexistent/orca",
	)
	if out, err := cmdOK.CombinedOutput(); err != nil {
		t.Fatalf("expected 12000-char STATUS.md to pass next-round.sh, got err=%v, out=%s", err, string(out))
	}

	overStatusPath := filepath.Join(tmpDir, "STATUS-12001.md")
	if err := os.WriteFile(overStatusPath, []byte(strings.Repeat("验", MaxStatusChars+1)), 0o600); err != nil {
		t.Fatalf("write over status: %v", err)
	}
	cmdOver := exec.Command("bash", scriptPath, "--check-only", "--dry-run", "--prompt-file", promptPath)
	cmdOver.Dir = root
	cmdOver.Env = append(os.Environ(),
		"MEMCTL_STATUS_FILE="+overStatusPath,
		"ORCA_CLI_COMMAND=/nonexistent/orca",
	)
	outOver, err := cmdOver.CombinedOutput()
	if err == nil {
		t.Fatalf("expected 12001-char STATUS.md to fail next-round.sh, got exit 0, out=%s", string(outOver))
	}
	for _, want := range []string{"12001", "12000"} {
		if !strings.Contains(string(outOver), want) {
			t.Errorf("next-round.sh failure output %q missing %q", string(outOver), want)
		}
	}
}
