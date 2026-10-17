package dream

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gray0128/memctl/internal/memerr"
)

// result captures the bounded standard output and error of one runner run.
type result struct {
	stdout    string
	stderr    string
	truncated bool
	exitCode  int
}

// runRunner starts the runner under the platform sandbox. The sandbox is the
// only thing that makes the main repository and the user configuration
// non-writable; without an available backend the call fails with UNSUPPORTED
// instead of falling back to an unrestricted shell.
func (s *Service) runRunner(ctx context.Context, workDir, execPath string, args []string) (result, error) {
	sandbox := s.Sandbox
	if sandbox == nil || !sandbox.Available() {
		return result{}, memerr.New(memerr.Unsupported,
			"no verified runner isolation backend is available on this platform; Dreaming generation is disabled")
	}
	executable, wrapped, err := sandbox.Wrap(workDir, execPath, args)
	if err != nil {
		return result{}, err
	}
	if err := os.MkdirAll(filepath.Join(workDir, "home"), 0o700); err != nil {
		return result{}, memerr.Wrap(memerr.IOFailed, err, "cannot create the runner home directory")
	}
	if err := os.MkdirAll(filepath.Join(workDir, "tmp"), 0o700); err != nil {
		return result{}, memerr.Wrap(memerr.IOFailed, err, "cannot create the runner temp directory")
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(executable, wrapped...)
	cmd.Dir = repoDir(workDir)
	cmd.Env = runnerEnv(workDir)
	cmd.Stdin = nil
	limit := s.MaxOutputBytes
	if limit <= 0 {
		limit = MaxRunnerOutput
	}
	stdout := &boundedWriter{limit: limit}
	stderr := &boundedWriter{limit: limit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return result{}, memerr.Wrap(memerr.IOFailed, err,
			"cannot start runner %s under %s", execPath, sandbox.Name())
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		out := result{stdout: stdout.String(), stderr: stderr.String(), truncated: stdout.overflow || stderr.overflow}
		if cmd.ProcessState != nil {
			out.exitCode = cmd.ProcessState.ExitCode()
		}
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return out, memerr.New(memerr.IOFailed,
					"runner %s exited with status %d", execPath, out.exitCode)
			}
			return out, memerr.Wrap(memerr.IOFailed, err, "runner %s failed to run", execPath)
		}
		return out, nil
	case <-runCtx.Done():
		killProcessGroup(cmd)
		<-done
		return result{stdout: stdout.String(), stderr: stderr.String(), truncated: stdout.overflow || stderr.overflow, exitCode: -1},
			memerr.New(memerr.Timeout, "runner %s exceeded the %s isolation time budget", execPath, timeout)
	}
}

// runnerEnv builds the minimal environment of one runner run. Only non-secret
// variables are inherited; HOME and TMPDIR point into the writable work
// directory, so the runner never writes the real user configuration. Host
// credential variables are deliberately not passed: a host-specific runner
// environment is exactly what a verified runner/platform combination has to
// establish before its runner is treated as supported.
func runnerEnv(workDir string) []string {
	env := []string{
		"HOME=" + filepath.Join(workDir, "home"),
		"TMPDIR=" + filepath.Join(workDir, "tmp"),
		"PWD=" + repoDir(workDir),
	}
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "TERM", "USER", "SHELL"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// boundedWriter collects at most limit bytes and records further writes as an
// overflow instead of failing the child.
type boundedWriter struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.overflow = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buffer.Write(p[:remaining])
		b.overflow = true
		return len(p), nil
	}
	b.buffer.Write(p)
	return len(p), nil
}

func (b *boundedWriter) String() string { return b.buffer.String() }
