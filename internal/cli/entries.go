package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/gray0128/memctl/internal/app"
)

// runEntries implements `memctl entries`. It exposes the same read-only entry
// query the Web UI uses through GET /api/v1/entries and the memory_entries MCP
// tool, so an agent, a script and a human read one implementation. It never
// writes the repository.
//
// Matching is a case-insensitive substring over the entry fact text; this is a
// lookup over already-stored facts, not a semantic search index. See
// docs/04-ui-and-cli-specification.md section 1 and
// docs/09-mcp-memory-tools.md section 5.
func runEntries(args []string, stdout, stderr io.Writer) int {
	query := app.EntryQuery{}
	asJSON := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		value := func(flag string) (string, bool) {
			if index+1 >= len(args) {
				usageError(stderr, "entries", flag+" requires a value")
				return "", false
			}
			index++
			return args[index], true
		}
		switch {
		case arg == "--json":
			asJSON = true
		case arg == "--q":
			got, ok := value("--q")
			if !ok {
				return ExitUsageError
			}
			query.Q = got
		case arg == "--file":
			got, ok := value("--file")
			if !ok {
				return ExitUsageError
			}
			query.File = got
		case arg == "--agent":
			got, ok := value("--agent")
			if !ok {
				return ExitUsageError
			}
			query.Agent = got
		case arg == "--device":
			got, ok := value("--device")
			if !ok {
				return ExitUsageError
			}
			query.Device = got
		case arg == "--from":
			got, ok := value("--from")
			if !ok {
				return ExitUsageError
			}
			query.From = got
		case arg == "--to":
			got, ok := value("--to")
			if !ok {
				return ExitUsageError
			}
			query.To = got
		case arg == "--limit":
			got, ok := value("--limit")
			if !ok {
				return ExitUsageError
			}
			limit, err := strconv.Atoi(got)
			if err != nil {
				return usageError(stderr, "entries", fmt.Sprintf("--limit must be an integer, got %q", got))
			}
			query.Limit = limit
		case arg == "--offset":
			got, ok := value("--offset")
			if !ok {
				return ExitUsageError
			}
			offset, err := strconv.Atoi(got)
			if err != nil {
				return usageError(stderr, "entries", fmt.Sprintf("--offset must be an integer, got %q", got))
			}
			query.Offset = offset
		default:
			return usageError(stderr, "entries", fmt.Sprintf("unknown argument %q", arg))
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "entries", err)
	}
	page, err := app.New(cfg).ListEntries(context.Background(), query)
	if err != nil {
		return fail(stderr, "entries", err)
	}

	if asJSON {
		if err := encodeJSON(stdout, page); err != nil {
			return fail(stderr, "entries", err)
		}
		return ExitOK
	}
	if len(page.Items) == 0 {
		fmt.Fprintf(stdout, "no matching entries (revision %s)\n", page.Revision)
		return ExitOK
	}
	// Text output is one entry per line as "path:line\tfact", which stays
	// grep- and awk-friendly without inventing a second serialization.
	for _, entry := range page.Items {
		fmt.Fprintf(stdout, "%s:%d\t%s\n", entry.FilePath, entry.LineNumber, entry.FactText)
	}
	if end := query.Offset + len(page.Items); end < page.Total {
		fmt.Fprintf(stdout, "--- %d of %d entries; continue with --offset %d\n", end, page.Total, end)
	} else {
		fmt.Fprintf(stdout, "--- %d of %d entries\n", page.Total, page.Total)
	}
	return ExitOK
}
