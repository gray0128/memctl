package dream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/memerr"
)

// This file verifies D05 of docs/05-validation-and-delivery.md: the Actions
// worker extracts static candidates, calls the model in bounded steps, corrects
// one invalid JSON response, publishes a failed result when the provider fails
// and never lets a step escape its batch. It also verifies the four provider
// transports against local HTTP servers, so no model endpoint is touched.

// workerRepo creates a memory repository with a bare origin and returns both.
func workerRepo(t *testing.T) (string, string) {
	return initRepo(t)
}

// workerService binds a worker service to the repository with an injected
// provider.
func workerService(t *testing.T, repo string, provider Provider) *Service {
	t.Helper()
	service := NewService(Source{RepoPath: repo, RepoID: config.RepoID(repo), StateDir: t.TempDir()})
	service.NewProvider = func(string) (Provider, error) { return provider, nil }
	return service
}

// publishRequest pushes the request ref the workflow reacts to.
func publishRequest(t *testing.T, repo, bare, proposalID, provider, baseHead string) {
	t.Helper()
	env := gitEnv(t)
	gitRun(t, env, repo, "push", "-q", bare, baseHead+":"+RequestRef(proposalID, provider))
}

// resultMeta reads the worker metadata from the published result branch.
func resultMeta(t *testing.T, repo, proposalID string) (WorkerMeta, string) {
	t.Helper()
	env := gitEnv(t)
	gitRun(t, env, repo, "fetch", "-q", "origin", ResultRef(proposalID)+":"+ResultRef(proposalID))
	head := gitRun(t, env, repo, "rev-parse", ResultRef(proposalID))
	message := gitRun(t, env, repo, "log", "-1", "--format=%B", head)
	meta, err := parseWorkerMeta(message)
	if err != nil {
		t.Fatalf("parse worker meta: %v", err)
	}
	return meta, head
}

const validStep = `{"operations":[{"action":"update","path":"MEMORY.md","content":"# Memory\n\n## Index\n- [[topic]]\n- [[extra]]\n","rationale":"keep the index current"},{"action":"create","path":"extra.md","content":"# Extra\n- added [source: test; added: 2026-10-19]\n","rationale":"split the topic"}],"warnings":["review the split"]}`

func TestWorkerPublishesReadyResultCoversD05(t *testing.T) {
	repo, bare := workerRepo(t)
	provider := &fakeProvider{responses: []string{validStep}}
	service := workerService(t, repo, provider)
	base := gitRun(t, gitEnv(t), repo, "rev-parse", "HEAD")
	const proposalID = "dp-0123456789abcdef0123456789abcdef"
	publishRequest(t, repo, bare, proposalID, ProviderDeepSeek, base)

	result, err := service.Worker(context.Background(), RequestRef(proposalID, ProviderDeepSeek), base)
	if err != nil {
		t.Fatalf("Worker: %v", err)
	}
	meta0, _ := resultMeta(t, repo, proposalID)
	if result.Status != WorkerStatusReady || result.Commit == "" {
		t.Fatalf("result = %+v (error=%q), want ready with a commit", result, meta0.Error)
	}
	meta, head := resultMeta(t, repo, proposalID)
	if meta.Status != WorkerStatusReady || meta.BaseHead != base || meta.ProposalID != proposalID {
		t.Fatalf("meta = %+v", meta)
	}
	if meta.Model != "fake-model" || meta.Provider != ProviderDeepSeek {
		t.Fatalf("meta identity = %+v", meta)
	}
	if len(meta.Warnings) != 1 || meta.Warnings[0] != "review the split" {
		t.Fatalf("meta warnings = %v", meta.Warnings)
	}
	// The single documented parent rule.
	if parents := gitRun(t, gitEnv(t), repo, "rev-list", "--parents", "-n", "1", head); len(strings.Fields(parents)) != 2 {
		t.Fatalf("result commit parents = %q, want one", parents)
	}
	tree := gitRun(t, gitEnv(t), repo, "show", head+":extra.md")
	if !strings.Contains(tree, "- added") {
		t.Fatalf("result tree is missing the new file: %q", tree)
	}
	// The request payload carried the static findings, not only the content.
	if len(provider.requests) != 1 {
		t.Fatalf("model calls = %d, want 1", len(provider.requests))
	}
	var payload workerRequest
	if err := json.Unmarshal([]byte(provider.requests[0].User), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.StaticFindings) == 0 {
		t.Fatalf("payload has no static findings: %s", provider.requests[0].User)
	}
	if payload.Today == "" || payload.Phase != "batch" || len(payload.BatchFiles) == 0 {
		t.Fatalf("payload shape = %+v", payload)
	}
	if !strings.Contains(provider.requests[0].System, "Event-like memory") {
		t.Fatalf("system prompt does not carry the event-memory rule")
	}
}

func TestWorkerCorrectsOneInvalidResponseCoversD05(t *testing.T) {
	repo, bare := workerRepo(t)
	provider := &fakeProvider{responses: []string{"here you go: ```json\n{\"operations\": [}\n```", validStep}}
	service := workerService(t, repo, provider)
	base := gitRun(t, gitEnv(t), repo, "rev-parse", "HEAD")
	const proposalID = "dp-11111111111111111111111111111111"
	publishRequest(t, repo, bare, proposalID, ProviderOpenAI, base)

	result, err := service.Worker(context.Background(), RequestRef(proposalID, ProviderOpenAI), base)
	if err != nil {
		t.Fatalf("Worker: %v", err)
	}
	if result.Status != WorkerStatusReady {
		t.Fatalf("status = %q, want ready after one correction", result.Status)
	}
	if provider.calls != 2 {
		t.Fatalf("model calls = %d, want 2", provider.calls)
	}
	var second workerRequest
	if err := json.Unmarshal([]byte(provider.requests[1].User), &second); err != nil {
		t.Fatalf("decode correction payload: %v", err)
	}
	if second.Correction == "" {
		t.Fatalf("the retry did not carry a correction message")
	}
}

func TestWorkerPublishesFailureWhenProviderFailsCoversD05(t *testing.T) {
	repo, bare := workerRepo(t)
	secret := "sk-live-should-never-leak"
	provider := &fakeProvider{err: memerr.New(memerr.AuthFailed, "anthropic returned HTTP 401: %s is invalid", secret)}
	service := workerService(t, repo, provider)
	base := gitRun(t, gitEnv(t), repo, "rev-parse", "HEAD")
	const proposalID = "dp-22222222222222222222222222222222"
	publishRequest(t, repo, bare, proposalID, ProviderAnthropic, base)

	result, err := service.Worker(context.Background(), RequestRef(proposalID, ProviderAnthropic), base)
	if err != nil {
		t.Fatalf("Worker must publish a failure result instead of failing: %v", err)
	}
	if result.Status != WorkerStatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	meta, _ := resultMeta(t, repo, proposalID)
	if meta.Status != WorkerStatusFailed || !strings.Contains(meta.Error, "HTTP 401") {
		t.Fatalf("meta = %+v, want the provider reason", meta)
	}
	if strings.Contains(meta.Error, "sk-") {
		t.Fatalf("the worker leaked a credential into the result commit: %q", meta.Error)
	}
}

func TestWorkerRejectsIllegalActionCoversD05(t *testing.T) {
	repo, bare := workerRepo(t)
	illegal := `{"operations":[{"action":"rename","path":"topic.md","content":"# Topic\n","rationale":"bad"}],"warnings":[]}`
	provider := &fakeProvider{responses: []string{illegal, illegal}}
	service := workerService(t, repo, provider)
	base := gitRun(t, gitEnv(t), repo, "rev-parse", "HEAD")
	const proposalID = "dp-33333333333333333333333333333333"
	publishRequest(t, repo, bare, proposalID, ProviderMiniMax, base)

	result, err := service.Worker(context.Background(), RequestRef(proposalID, ProviderMiniMax), base)
	if err != nil {
		t.Fatalf("Worker: %v", err)
	}
	if result.Status != WorkerStatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	meta, _ := resultMeta(t, repo, proposalID)
	if !strings.Contains(meta.Error, "did not satisfy the step contract") {
		t.Fatalf("meta error = %q, want the step-contract rejection", meta.Error)
	}
}

// TestStepGuardRefusesProtectedAndEntryPaths verifies the deterministic guard
// directly, so the rule is covered without a full worker run.
func TestStepGuardRefusesProtectedAndEntryPaths(t *testing.T) {
	repo, _ := workerRepo(t)
	env := gitEnv(t)
	files := &fileSet{files: map[string][]byte{
		"MEMORY.md": []byte("# Memory\n\n## Index\n- [[topic]]\n"),
		"topic.md":  []byte("# Topic\n"),
		"other.md":  []byte("# Other\n"),
	}}
	w := &worker{Now: func() time.Time { return time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC) }}
	w.known = baselineProblems(files)
	w.introduced = map[string]bool{}

	cases := map[string]DreamStepOutput{
		"workflow": {
			Operations: []StepOperation{{Action: StepUpdate, Path: WorkflowRelativePath, Content: "x", Rationale: "r"}},
		},
		"delete entry": {
			Operations: []StepOperation{{Action: StepDelete, Path: "MEMORY.md", Rationale: "r"}},
		},
		"outside batch": {
			Operations: []StepOperation{{Action: StepUpdate, Path: "other.md", Content: "# Other\n- x\n", Rationale: "r"}},
		},
		"dangling link": {
			Operations: []StepOperation{{Action: StepUpdate, Path: "topic.md", Content: "# Topic\n- [[missing]]\n", Rationale: "r"}},
		},
	}
	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := files.clone()
			w.introduced = map[string]bool{}
			if err := w.applyStep(candidate, output, []string{"topic.md"}); err == nil {
				t.Fatalf("%s step was accepted", name)
			}
		})
	}
	// The pre-existing link in MEMORY.md is not a step failure once the step
	// keeps it unchanged.
	if problems := baselineProblems(files); len(problems) != 0 {
		t.Fatalf("fixture has unexpected link problems: %v", problems)
	}
	_ = repo
	_ = env
}

// TestProvidersEmitDocumentedRequestShapes covers the four provider transports:
// endpoint, authentication header, structured-output field and response
// decoding. Each provider talks to a local HTTP server.
func TestProvidersEmitDocumentedRequestShapes(t *testing.T) {
	cases := []struct {
		id       string
		path     string
		check    func(t *testing.T, body map[string]any, header http.Header)
		response string
	}{
		{
			id:   ProviderDeepSeek,
			path: "/chat/completions",
			check: func(t *testing.T, body map[string]any, header http.Header) {
				if header.Get("Authorization") != "Bearer deepseek-key" {
					t.Fatalf("authorization = %q", header.Get("Authorization"))
				}
				format, _ := body["response_format"].(map[string]any)
				if format["type"] != "json_object" {
					t.Fatalf("response_format = %v, want json_object", format)
				}
			},
			response: `{"choices":[{"message":{"content":"{\"operations\":[],\"warnings\":[]}"}}]}`,
		},
		{
			id:   ProviderMiniMax,
			path: "/v1/chat/completions",
			check: func(t *testing.T, body map[string]any, _ http.Header) {
				if body["stream"] != false {
					t.Fatalf("stream = %v, want false", body["stream"])
				}
				if body["model"] != "MiniMax-M2.7" {
					t.Fatalf("model = %v", body["model"])
				}
			},
			response: `{"choices":[{"message":{"content":"{\"operations\":[],\"warnings\":[]}"}}]}`,
		},
		{
			id:   ProviderOpenAI,
			path: "/v1/chat/completions",
			check: func(t *testing.T, body map[string]any, _ http.Header) {
				format, _ := body["response_format"].(map[string]any)
				schema, _ := format["json_schema"].(map[string]any)
				if format["type"] != "json_schema" || schema["strict"] != true {
					t.Fatalf("response_format = %v, want a strict json_schema", format)
				}
			},
			response: `{"choices":[{"message":{"content":"{\"operations\":[],\"warnings\":[]}"}}]}`,
		},
		{
			id:   ProviderAnthropic,
			path: "/v1/messages",
			check: func(t *testing.T, body map[string]any, header http.Header) {
				if header.Get("x-api-key") != "anthropic-key" {
					t.Fatalf("x-api-key = %q", header.Get("x-api-key"))
				}
				if header.Get("anthropic-version") != "2023-06-01" {
					t.Fatalf("anthropic-version = %q", header.Get("anthropic-version"))
				}
				choice, _ := body["tool_choice"].(map[string]any)
				if choice["type"] != "tool" || choice["name"] != "emit_dream_step" {
					t.Fatalf("tool_choice = %v", choice)
				}
			},
			response: `{"content":[{"type":"tool_use","name":"emit_dream_step","input":{"operations":[],"warnings":[]}}]}`,
		},
	}
	keys := map[string]string{
		ProviderDeepSeek:  "deepseek-key",
		ProviderMiniMax:   "minimax-key",
		ProviderOpenAI:    "openai-key",
		ProviderAnthropic: "anthropic-key",
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("path = %q, want %q", r.URL.Path, tc.path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("method = %q", r.Method)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
				}
				tc.check(t, body, r.Header)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			provider, err := NewProvider(ProviderOptions{
				ID:      tc.id,
				APIKey:  keys[tc.id],
				BaseURL: server.URL + tc.path,
			})
			if err != nil {
				t.Fatalf("NewProvider: %v", err)
			}
			text, err := provider.Complete(context.Background(), CompletionRequest{
				System: "system",
				User:   "{}",
				Schema: StepSchema(),
			})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if !strings.Contains(text, "operations") {
				t.Fatalf("text = %q", text)
			}
		})
	}
}

func TestProviderErrorsAreTargeted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key sk-abc"}}`))
	}))
	defer server.Close()

	provider, err := NewProvider(ProviderOptions{ID: ProviderOpenAI, APIKey: "openai-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	_, err = provider.Complete(context.Background(), CompletionRequest{System: "s", User: "u", Schema: StepSchema()})
	contract, ok := memerr.As(err)
	if !ok || contract.Code != memerr.AuthFailed {
		t.Fatalf("err = %v, want AUTH_FAILED", err)
	}
	if strings.Contains(err.Error(), "sk-") {
		t.Fatalf("the echoed body was not sanitized: %q", err.Error())
	}

	if _, err := NewProvider(ProviderOptions{ID: ProviderDeepSeek}); !contractCode(err, "AUTH_FAILED") {
		t.Fatalf("missing key: err=%v, want AUTH_FAILED", err)
	}
	if _, err := NewProvider(ProviderOptions{ID: "claude", APIKey: "x"}); !contractCode(err, "UNSUPPORTED") {
		t.Fatalf("unknown provider: err=%v, want UNSUPPORTED", err)
	}
}

func TestWorkerRejectsWrongBaseline(t *testing.T) {
	repo, _ := workerRepo(t)
	service := workerService(t, repo, &fakeProvider{})
	const proposalID = "dp-44444444444444444444444444444444"
	_, err := service.Worker(context.Background(), RequestRef(proposalID, ProviderDeepSeek), "0000000000000000000000000000000000000000")
	if !contractCode(err, "REVISION_MISMATCH") {
		t.Fatalf("err = %v, want REVISION_MISMATCH", err)
	}
	var contract *memerr.Error
	if !errors.As(err, &contract) || contract.Code != memerr.RevisionMismatch {
		t.Fatalf("err = %v, want a contract REVISION_MISMATCH", err)
	}
}
