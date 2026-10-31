package corpus

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
)

// DeltaCLI is the executable name the Delta collector starts. The adapter
// already detects this host by the same name, so an operator who sees Delta as
// configured in `install --status` has the binary this collector needs.
const DeltaCLI = "delta"

// DeltaCLIRunner returns a DeltaRunner that starts
// `delta thread watch <id> --json` and streams its stdout. The child never exits
// on its own: the watch stays connected after the stream's live marker, so the
// runner's own budget has to end it.
func DeltaCLIRunner(path string) DeltaRunner {
	if path == "" {
		path = DeltaCLI
	}
	return func(ctx context.Context, threadID string) (io.ReadCloser, error) {
		// The budget is attached here rather than relying on the caller, so a
		// caller that forgot one still cannot leave a child running.
		ctx, cancel := context.WithTimeout(ctx, DeltaCollectBudget)
		cmd := exec.CommandContext(ctx, path, "thread", "watch", threadID, "--json")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			return nil, err
		}
		configureDeltaProcessGroup(cmd)
		if err := cmd.Start(); err != nil {
			cancel()
			return nil, err
		}

		stream := &deltaStream{stdout: stdout, cmd: cmd, cancel: cancel, stopped: make(chan struct{})}
		go func() {
			// The budget ends the read even while the collector goroutine is
			// blocked on the pipe: stopping kills the whole process group, which
			// closes the write end of the pipe.
			<-ctx.Done()
			stream.stop()
		}()
		return stream, nil
	}
}

// deltaStream owns one watch child. Reading and stopping run on different
// goroutines, so both are safe to call concurrently.
type deltaStream struct {
	stdout io.ReadCloser
	cmd    *exec.Cmd
	cancel context.CancelFunc
	// stopped is closed by stop, which lets Read turn the resulting pipe error
	// into a plain end of stream.
	stopped   chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once
}

// Read implements io.Reader. A pipe closed by stop reports io.EOF: the collector
// then sees a stream without its live marker and skips the digest, which is
// exactly the documented handling of an incomplete read.
func (s *deltaStream) Read(p []byte) (int, error) {
	n, err := s.stdout.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		select {
		case <-s.stopped:
			return n, io.EOF
		default:
		}
	}
	return n, err
}

// stop kills the child's process group and reaps it. It is idempotent.
func (s *deltaStream) stop() {
	s.stopOnce.Do(func() {
		close(s.stopped)
		s.stdout.Close()
		s.cancel()
		killDeltaProcessGroup(s.cmd)
		_ = s.cmd.Wait()
	})
}

// Close implements io.Closer and never reports an error: the collector's
// contract is best effort, and a child that already exited is not a failure.
func (s *deltaStream) Close() error {
	s.closeOnce.Do(s.stop)
	return nil
}
