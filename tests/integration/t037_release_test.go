package integration_test

import (
	"net/http"
	"strings"
	"testing"
)

// This file adds the T037 (D-2 subtask 2) release check that contract tests
// cover every documented REST route. docs/05-validation-and-delivery.md
// section 4 requires the documented routes to match the implementation. The
// existing stage tests exercise each route's behavior; this test asserts route
// registration itself, so a route dropped from the server fails the check even
// if no behavior test happens to reach it.
//
// A TRACE request is used because no route registers it: Go's ServeMux answers
// 405 with an Allow header for a registered path with an unregistered method,
// and 404 for an unknown path. That proves registration without side effects.

// documentedRoutes mirrors the route table in
// docs/02-domain-models-and-contracts.md section 4.2 plus the embedded UI
// routes from docs/04-ui-and-cli-specification.md section 5.
var documentedRoutes = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/api/v1/snapshot"},
	{http.MethodGet, "/api/v1/files/alpha.md"},
	{http.MethodPut, "/api/v1/files/alpha.md"},
	{http.MethodPost, "/api/v1/files:move"},
	{http.MethodGet, "/api/v1/entries"},
	{http.MethodPost, "/api/v1/entries"},
	{http.MethodPatch, "/api/v1/entries/entry-1"},
	{http.MethodDelete, "/api/v1/entries/entry-1"},
	{http.MethodPost, "/api/v1/sync"},
	{http.MethodGet, "/api/v1/conflict"},
	{http.MethodPost, "/api/v1/conflict:resolve"},
	{http.MethodPost, "/api/v1/conflict:abort"},
	{http.MethodGet, "/api/v1/audit"},
	{http.MethodGet, "/api/v1/audit/deadbeef/diff"},
	{http.MethodPost, "/api/v1/audit/deadbeef/revert"},
	{http.MethodGet, "/api/v1/doctor"},
	{http.MethodPost, "/api/v1/doctor:fix"},
	{http.MethodPost, "/api/v1/dream:preview"},
	{http.MethodPost, "/api/v1/dream:apply"},
	{http.MethodGet, "/api/v1/dream/proposal-1"},
	{http.MethodGet, "/api/v1/adapters"},
	{http.MethodPost, "/api/v1/adapters/claude-code/install"},
	{http.MethodGet, "/api/v1/events"},
	{http.MethodGet, "/"},
	{http.MethodGet, "/assets/"},
}

// TestRoutesContractCoversT037 verifies that every documented route is
// registered with its documented method.
func TestRoutesContractCoversT037(t *testing.T) {
	fixture := newB1Fixture(t, false)

	for _, route := range documentedRoutes {
		target := "http://127.0.0.1:18420" + route.path
		res := fixture.do(t, http.MethodTrace, target, b1Token, "127.0.0.1:18420", "")
		if res.status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status=%d, want 405 (unregistered path or method)", route.method, route.path, res.status)
			continue
		}
		if allow := res.header.Get("Allow"); !strings.Contains(allow, route.method) {
			t.Errorf("%s %s: Allow=%q does not advertise %s", route.method, route.path, allow, route.method)
		}
	}
}
