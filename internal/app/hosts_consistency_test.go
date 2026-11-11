package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gray0128/memctl/internal/adapters"
	"github.com/gray0128/memctl/internal/corpus"
)

// corpusUnsupportedHosts lists the documented host adapters that do not yet
// have a verified local session-corpus file parser in internal/corpus:
//   - commandcode: transcript format unverified on local samples (docs/12 §2.2.1)
//   - grok: session storage format unverified on local samples (docs/12 §2.2.1)
//   - opencode: stores history in SQLite rather than JSONL files (docs/12 §2.2.1)
var corpusUnsupportedHosts = map[string]string{
	"commandcode": "unverified local transcript format (docs/12 section 2.2.1)",
	"grok":        "unverified local session format (docs/12 section 2.2.1)",
	"opencode":    "SQLite storage without a pure-Go JSONL parser (docs/12 section 2.2.1)",
}

// TestAdapterAndCorpusHostConsistencyCoversP0_3 verifies that adapters.Registry()
// and corpus host support stay in lockstep, and that LocateSession searches
// both ~/.gemini/antigravity/brain and ~/.gemini/antigravity-cli/brain.
func TestAdapterAndCorpusHostConsistencyCoversP0_3(t *testing.T) {
	adapterList := adapters.Registry()
	if len(adapterList) == 0 {
		t.Fatal("adapters.Registry() is empty")
	}
	for _, spec := range adapterList {
		_, exempt := corpusUnsupportedHosts[spec.Agent]
		supported := corpus.Supports(spec.Agent)
		if exempt && supported {
			t.Fatalf("host %q now supports corpus parsing; remove it from corpusUnsupportedHosts", spec.Agent)
		}
		if !exempt && !supported {
			t.Fatalf("host %q in adapters.Registry() is neither supported by corpus.Supports() nor documented in corpusUnsupportedHosts", spec.Agent)
		}
	}

	home := t.TempDir()
	cliTranscript := filepath.Join(home, ".gemini", "antigravity-cli", "brain", "sid-cli", ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(cliTranscript), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cliTranscript, []byte(`{"type":"USER_INPUT"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := corpus.LocateSession("antigravity", "sid-cli", "/work/memctl", home); got != cliTranscript {
		t.Fatalf("LocateSession(antigravity, sid-cli) = %q, want %q", got, cliTranscript)
	}
}
