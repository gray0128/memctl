package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gray0128/memctl/internal/mcp"
)

// printMCPStatus reports whether the running binary serves the memory tools and
// prints one registration action per host. It never writes a file and never
// runs a host command. A binary that cannot answer tools/list is reported with
// its reason and no registration commands, so an operator does not register a
// server that would fail every call. The probe and the registration table live
// in internal/mcp so the Web UI reports the same status. See M09.
func printMCPStatus(ctx context.Context, stdout io.Writer, memctl string) {
	status, err := mcp.StatusEnv{
		Executable: func() (string, error) { return memctl, nil },
	}.Status(ctx)
	if err != nil {
		fmt.Fprintf(stdout, "mcp: unavailable: %v\n", err)
		return
	}
	if !status.Available {
		fmt.Fprintf(stdout, "mcp: unavailable: %s\n", status.Reason)
		fmt.Fprintf(stdout, "  %s does not answer \"mcp\"; reinstall the binary before registering the memory tools\n", memctl)
		return
	}
	serving := "serving " + strings.Join(status.Tools, ", ")
	if status.Version != "" {
		serving += " (" + status.Version + ")"
	}
	fmt.Fprintf(stdout, "mcp: %s\n", serving)
	for _, registration := range status.Registrations {
		if registration.Command != "" {
			fmt.Fprintf(stdout, "  %s: %s\n", registration.Agent, registration.Command)
			continue
		}
		fmt.Fprintf(stdout, "  %s: %s\n", registration.Agent, registration.Instruction)
	}
}

// selfExecutable returns the running binary path used in registration commands.
func selfExecutable() (string, error) {
	return os.Executable()
}
