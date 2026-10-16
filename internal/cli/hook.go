package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/hookio"
	"github.com/gray0128/memctl/internal/memerr"
)

// hookSyncBudget is the documented remote budget for a host-triggered hook,
// per docs/04-ui-and-cli-specification.md section 3.1.
const hookSyncBudget = 8 * time.Second

// sessionFlags is the parsed flag set shared by start, context and pull.
type sessionFlags struct {
	agent    string
	sid      string
	useStdin bool
	asJSON   bool
	once     bool
}

// parseSessionFlags parses the Hook command flags. allowJSON is false for pull;
// allowOnce is false for start.
func parseSessionFlags(cmd string, args []string, allowJSON, allowOnce bool, stderr io.Writer) (sessionFlags, int) {
	var flags sessionFlags
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--stdin":
			flags.useStdin = true
		case arg == "--json":
			if !allowJSON {
				return flags, usageError(stderr, cmd, "--json is not supported for this command")
			}
			flags.asJSON = true
		case arg == "--once":
			if !allowOnce {
				return flags, usageError(stderr, cmd, "--once is not supported for this command")
			}
			flags.once = true
		case arg == "--agent" || arg == "--sid":
			if index+1 >= len(args) {
				return flags, usageError(stderr, cmd, arg+" requires a value")
			}
			index++
			if arg == "--agent" {
				flags.agent = args[index]
			} else {
				flags.sid = args[index]
			}
		case strings.HasPrefix(arg, "--agent="):
			flags.agent = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--sid="):
			flags.sid = strings.TrimPrefix(arg, "--sid=")
		default:
			return flags, usageError(stderr, cmd, fmt.Sprintf("unknown argument %q", arg))
		}
	}
	return flags, ExitOK
}

// runStart implements `start --agent NAME --sid ID --stdin --json`, the
// SessionStart hook.
func runStart(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags, code := parseSessionFlags("start", args, true, false, stderr)
	if code != ExitOK {
		return code
	}
	return runSessionCommand("start", flags, hookio.EventSessionStart, stdin, stdout, stderr)
}

// runContext implements `context [--once] --agent NAME --sid ID --stdin
// --json`, the UserPromptSubmit hook.
func runContext(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags, code := parseSessionFlags("context", args, true, true, stderr)
	if code != ExitOK {
		return code
	}
	return runSessionCommand("context", flags, hookio.EventUserPromptSubmit, stdin, stdout, stderr)
}

// runPull implements `pull --agent NAME --sid ID --stdin --once`. It uses the
// same synchronization flow as start but writes nothing to stdout.
func runPull(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags, code := parseSessionFlags("pull", args, false, true, stderr)
	if code != ExitOK {
		return code
	}
	return runSessionCommand("pull", flags, "", stdin, stdout, stderr)
}

func runSessionCommand(cmd string, flags sessionFlags, event string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, cmd, err)
	}

	var payload hookio.Input
	if flags.useStdin {
		payload, err = hookio.ReadInput(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "memctl %s: %v\n", cmd, err)
			return ExitUsageError
		}
	}
	agent := hookio.ResolveAgent(flags.agent, os.Getenv)
	sid := hookio.ResolveSID(flags.sid, os.Getenv, payload)

	service := app.New(cfg)
	outcome, err := service.PrepareSession(context.Background(), app.SessionOptions{
		Agent:              agent,
		SessionID:          sid,
		Once:               flags.once,
		NoBody:             cmd == "pull",
		SkipSyncWhenSynced: cmd == "context" || cmd == "pull",
		Interactive:        !flags.useStdin && hasTerminal(),
		NonBlockingLock:    flags.useStdin,
		Budget:             sessionBudget(flags.useStdin),
	})
	if err != nil {
		if flags.useStdin {
			fmt.Fprintf(stderr, "memctl %s: %v\n", cmd, err)
			return ExitOK
		}
		return fail(stderr, cmd, err)
	}

	if cmd != "pull" && outcome.Inject {
		if flags.asJSON {
			if err := hookio.Encode(stdout, event, outcome.Body); err != nil {
				return fail(stderr, cmd, err)
			}
		} else if _, err := io.WriteString(stdout, outcome.Body); err != nil {
			return fail(stderr, cmd, err)
		}
		service.MarkSessionInjected(agent, sid)
	}

	if !flags.useStdin && outcome.HookFailed {
		fmt.Fprintf(stderr, "memctl %s: %s\n", cmd, sessionFailureText(outcome))
		return ExitError
	}
	return ExitOK
}

func sessionBudget(hook bool) time.Duration {
	if hook {
		return hookSyncBudget
	}
	return 0
}

func sessionFailureText(out app.SessionOutcome) string {
	if out.SyncState == app.SessionSyncNoRepo {
		return "NO_REPO: no memory repository at " + out.RepoPath + "; run `memctl init`"
	}
	if out.ReasonCode != "" {
		return out.ReasonCode + ": remote=" + out.RemoteState
	}
	return "sync failed: remote=" + out.RemoteState
}

// runTag implements `tag --agent NAME --sid ID`. It prints the canonical
// source URI and never guesses a session from shared state.
func runTag(args []string, stdout, stderr io.Writer) int {
	agentFlag, sidFlag := "", ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--agent" || arg == "--sid":
			if index+1 >= len(args) {
				return usageError(stderr, "tag", arg+" requires a value")
			}
			index++
			if arg == "--agent" {
				agentFlag = args[index]
			} else {
				sidFlag = args[index]
			}
		case strings.HasPrefix(arg, "--agent="):
			agentFlag = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--sid="):
			sidFlag = strings.TrimPrefix(arg, "--sid=")
		default:
			return usageError(stderr, "tag", fmt.Sprintf("unknown argument %q", arg))
		}
	}

	agent := hookio.ResolveAgent(agentFlag, os.Getenv)
	sid := hookio.ResolveSID(sidFlag, os.Getenv, hookio.Input{})
	if sid == "" {
		fmt.Fprintln(stderr, "memctl tag: NO_SESSION_ID: a stable session id is required; pass --sid or set AGENT_MEMORY_SID")
		return ExitError
	}
	fmt.Fprintln(stdout, hookio.Tag(hookio.ResolveDevice(os.Getenv), agent, sid))
	return ExitOK
}

// runWrap implements `wrap --agent NAME -- <cmd...>`: pre-run synchronization,
// session environment for the child, post-run best-effort synchronization and
// the child's exit status, which no sync result may overwrite.
func runWrap(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	agentFlag, sidFlag := "", ""
	var cmdArgs []string
	afterSeparator := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case afterSeparator:
			cmdArgs = append(cmdArgs, arg)
		case arg == "--":
			afterSeparator = true
		case arg == "--agent" || arg == "--sid":
			if index+1 >= len(args) {
				return usageError(stderr, "wrap", arg+" requires a value")
			}
			index++
			if arg == "--agent" {
				agentFlag = args[index]
			} else {
				sidFlag = args[index]
			}
		case strings.HasPrefix(arg, "--agent="):
			agentFlag = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--sid="):
			sidFlag = strings.TrimPrefix(arg, "--sid=")
		default:
			return usageError(stderr, "wrap", fmt.Sprintf("unknown argument %q before --", arg))
		}
	}
	if !afterSeparator || len(cmdArgs) == 0 {
		return usageError(stderr, "wrap", "wrap requires a command after --")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "wrap", err)
	}

	agent := hookio.ResolveAgent(agentFlag, os.Getenv)
	sid := hookio.ResolveSID(sidFlag, os.Getenv, hookio.Input{})
	if sid == "" {
		sid, err = newWrappedSessionID()
		if err != nil {
			return fail(stderr, "wrap", err)
		}
	}
	device := hookio.ResolveDevice(os.Getenv)

	// Pre-run synchronization: foreground, bounded, non-blocking and never
	// fatal to the child. A busy or offline repository must not delay the
	// wrapped command indefinitely.
	preCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_, preErr := app.New(cfg).SyncWithOptions(preCtx, app.SyncOptions{Interactive: hasTerminal(), Background: true})
	cancel()
	if preErr != nil {
		fmt.Fprintf(stderr, "memctl wrap: pre-run sync incomplete: %s\n", wrapSyncDetail(preErr))
	}
	fmt.Fprintf(stderr, "[agent-memory] source tag: %s\n", hookio.Tag(device, agent, sid))

	child := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	child.Stdin = stdin
	child.Stdout = stdout
	child.Stderr = stderr
	child.Env = wrappedEnv(cfg, agent, sid)

	runErr := child.Run()

	// Post-run synchronization is best-effort and detached so it cannot change
	// the wrapped program's exit status.
	if cfg.BackgroundSync {
		_ = app.New(cfg).BeginBackgroundSync()
	}

	code, ok := wrappedExitCode(runErr)
	if !ok {
		fmt.Fprintf(stderr, "memctl wrap: cannot run %q: %v\n", cmdArgs[0], runErr)
		return ExitError
	}
	return code
}

func wrapSyncDetail(err error) string {
	if code, ok := memerr.As(err); ok {
		return string(code.Code)
	}
	return "sync did not complete"
}

func newWrappedSessionID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot generate a wrapped session id")
	}
	return "wrap-" + hex.EncodeToString(raw[:]), nil
}

// wrappedEnv passes the session identity and canonical repository paths to the
// child without rewriting the rest of the caller's environment.
func wrappedEnv(cfg config.Config, agent, sid string) []string {
	env := os.Environ()
	env = setEnvValue(env, "AGENT_MEMORY_SID", sid)
	env = setEnvValue(env, "AGENT_MEMORY_AGENT", agent)
	env = setEnvValue(env, "AGENT_MEMORY_DIR", cfg.RepoPath)
	env = setEnvValue(env, "AGENT_MEMORY_STATE", cfg.StateDir)
	return env
}

func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	for index, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[index] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// drainHookStdin consumes a bounded Hook payload when --stdin was passed so a
// host writing to the pipe cannot block on a command that ignores the body.
func drainHookStdin(cmd string, stdin io.Reader, stderr io.Writer) int {
	if _, err := hookio.ReadInput(stdin); err != nil {
		fmt.Fprintf(stderr, "memctl %s: %v\n", cmd, err)
		return ExitUsageError
	}
	return ExitOK
}
