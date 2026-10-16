package vault_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// TestValidateRelPathCoversP01 checks scenario P01 from
// docs/05-validation-and-delivery.md: escapes, alias forms and Git internals
// are rejected by the single path boundary used for every access.
func TestValidateRelPathCoversP01(t *testing.T) {
	valid := []string{
		"MEMORY.md",
		"notes/index.md",
		"queries/long name.sql",
		"scripts/build.sh",
		"a/b/c/deep.txt",
		"CONSOLE.md",
		"NULLABLE.sql",
	}
	for _, path := range valid {
		t.Run("valid/"+path, func(t *testing.T) {
			got, err := vault.ValidateRelPath(path)
			if err != nil {
				t.Fatalf("ValidateRelPath(%q): %v", path, err)
			}
			if got != path {
				t.Fatalf("ValidateRelPath(%q) = %q, want the same path", path, got)
			}
		})
	}

	invalid := []string{
		"",
		"/etc/passwd",
		"../outside.md",
		"notes/../../outside.md",
		"a//b.md",
		"./MEMORY.md",
		"notes/./x.md",
		`notes\windows.md`,
		"notes\x00.md",
		"C:/memory/x.md",
		"c:memory",
		".git/config",
		".GIT/config",
		"nested/.git/HEAD",
		"NUL",
		"con.md",
		"AUX.txt",
		"COM1.md",
		"lpt9/log.txt",
		"notes/trailing.",
		"notes/trailing ",
		"notes/stream:name.md",
	}
	for index, path := range invalid {
		t.Run(fmt.Sprintf("invalid-%02d", index), func(t *testing.T) {
			t.Logf("path=%q", path)
			_, err := vault.ValidateRelPath(path)
			contractErr, ok := memerr.As(err)
			if !ok || contractErr.Code != memerr.InvalidPath {
				t.Fatalf("ValidateRelPath(%q) = %v, want INVALID_PATH", path, err)
			}
			if strings.Contains(contractErr.Message, "\x00") {
				t.Fatalf("error message must not echo a NUL byte: %q", contractErr.Message)
			}
		})
	}
}

func TestClassifyKind(t *testing.T) {
	cases := map[string]vault.Kind{
		"MEMORY.md":            vault.KindEntryPoint,
		"notes/index.md":       vault.KindMarkdown,
		"notes/notes.markdown": vault.KindMarkdown,
		"queries/report.sql":   vault.KindSQL,
		"scripts/build.sh":     vault.KindScript,
		"scripts/tool.py":      vault.KindScript,
		"assets/diagram.png":   vault.KindOther,
		"data/plain.txt":       vault.KindOther,
	}
	for path, want := range cases {
		if got := vault.ClassifyKind(path); got != want {
			t.Errorf("ClassifyKind(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFileRevisionMatchesDocumentedEncoding(t *testing.T) {
	// Independently computed over the length-prefixed mode and content fields
	// defined in docs/02-domain-models-and-contracts.md section 1.1.
	const want = "sha256:3d3ad089ac30852a08937bc697caebe8b0c7a39f8d15648b2a126ad61ad47f6a"
	got := vault.FileRevision("100644", []byte("# Memory\n\n## Index\n"))
	if got != want {
		t.Fatalf("FileRevision = %q, want %q", got, want)
	}
}

func TestFileRevisionDistinguishesModeAndContent(t *testing.T) {
	content := []byte("same bytes")
	regular := vault.FileRevision("100644", content)
	executable := vault.FileRevision("100755", content)
	if regular == executable {
		t.Fatal("file revision must include the Git file mode")
	}

	other := vault.FileRevision("100644", []byte("other bytes"))
	if regular == other {
		t.Fatal("file revision must include the content")
	}

	empty := vault.FileRevision("100644", nil)
	if empty == regular {
		t.Fatal("empty content must not collide with non-empty content")
	}
}
