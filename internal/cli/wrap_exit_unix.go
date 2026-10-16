//go:build unix

package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// wrappedExitCode maps a child process result to the wrapper exit code. A
// signal-terminated child keeps its signal semantics as 128+signal; ok is
// false when the program could not be started at all.
func wrappedExitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			return 128 + int(status.Signal()), true
		}
		return status.ExitStatus(), true
	}
	return exitErr.ExitCode(), true
}

// forwardSignals relays the termination signals a host or process manager sends
// to `memctl wrap` to the wrapped process, so the wrapped program keeps its
// shutdown semantics instead of being orphaned. The returned function stops the
// forwarding after the child has exited.
func forwardSignals(process *os.Process) func() {
	if process == nil {
		return func() {}
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-signals:
				_ = process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}
