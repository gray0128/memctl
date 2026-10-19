package server

import (
	"context"
	"net/http"

	"github.com/gray0128/memctl/internal/mcp"
)

// handleMCPStatus reports the status of the memctl MCP channel: whether the
// running binary serves the memory tools and which command registers it in
// each host. It is the HTTP counterpart of the `mcp:` block of
// `memctl install --status`, sharing one probe and one registration table so
// the CLI and the UI cannot drift. The route is read-only by contract: it
// resolves the executable and speaks the protocol, never reading host
// configuration or writing a file. See docs/09-mcp-memory-tools.md section 5.3.
func (s *Server) handleMCPStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.mcpStatus(r.Context())
	if err != nil {
		// Failing to resolve the running executable is an environment
		// problem, not an MCP failure: report it so the operator can fix the
		// path instead of reading an empty adapter list.
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, toMCPStatusDTO(status))
}

// mcpStatusDTO is the JSON shape of GET /api/v1/mcp. See
// docs/02-domain-models-and-contracts.md section 4.2.
type mcpStatusDTO struct {
	Executable    string               `json:"executable"`
	Available     bool                 `json:"available"`
	Version       string               `json:"version,omitempty"`
	Tools         []string             `json:"tools"`
	Reason        string               `json:"reason,omitempty"`
	Registrations []mcpRegistrationDTO `json:"registrations"`
}

// mcpRegistrationDTO is one host's registration action. Command is empty when
// the host has no equivalent CLI and the operator edits its configuration by
// hand, in which case Instruction carries the manual step.
type mcpRegistrationDTO struct {
	Agent       string `json:"agent"`
	Command     string `json:"command,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

func toMCPStatusDTO(status mcp.Status) mcpStatusDTO {
	registrations := make([]mcpRegistrationDTO, 0, len(status.Registrations))
	for _, registration := range status.Registrations {
		registrations = append(registrations, mcpRegistrationDTO{
			Agent:       registration.Agent,
			Command:     registration.Command,
			Instruction: registration.Instruction,
		})
	}
	return mcpStatusDTO{
		Executable:    status.Executable,
		Available:     status.Available,
		Version:       status.Version,
		Tools:         stringsOrEmpty(status.Tools),
		Reason:        status.Reason,
		Registrations: registrations,
	}
}

// probeMCPStatus is the default resolver used when Options.MCPStatus is nil. It
// derives the executable from the running process, which is the same binary
// `memctl ui` was started from.
func probeMCPStatus(ctx context.Context) (mcp.Status, error) {
	return mcp.DefaultStatusEnv().Status(ctx)
}
