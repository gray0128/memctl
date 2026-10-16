package server

import (
	"strings"
	"testing"
)

// TestSplitAddrLoopbackOnly verifies the server never advertises a non-loopback
// listen address.
func TestSplitAddrLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:18420", "localhost:18420", "[::1]:18420", "18420"} {
		if _, _, err := splitAddr(addr); err != nil {
			t.Fatalf("splitAddr(%q) should be allowed: %v", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:18420", "192.168.1.5:18420", "example.com:18420", "127.0.0.1", "not-an-address"} {
		if _, _, err := splitAddr(addr); err == nil {
			t.Fatalf("splitAddr(%q) should be rejected", addr)
		}
	}
}

func TestListenBindsLoopbackOnly(t *testing.T) {
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen(loopback) failed: %v", err)
	}
	listener.Close()
	if _, err := Listen("0.0.0.0:0"); err == nil {
		t.Fatal("Listen should refuse a non-loopback address")
	}
}

func TestValidateRequestPath(t *testing.T) {
	valid := []string{"/api/v1/snapshot", "/api/v1/files/alpha.md", "/api/v1/files/notes/deep.md", "/api/v1/files/C:x"}
	for _, path := range valid {
		if err := validateRequestPath(path); err != nil {
			t.Fatalf("path %q should be accepted: %v", path, err)
		}
	}
	invalid := []string{"/api/v1/files/../MEMORY.md", "/api/v1/files/a/./b", "/api/v1/files//etc/passwd", "/api/v1/files/a\\b"}
	for _, path := range invalid {
		if err := validateRequestPath(path); err == nil {
			t.Fatalf("path %q should be rejected", path)
		}
	}
}

func TestGenerateTokenIs32Bytes(t *testing.T) {
	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken failed: %v", err)
	}
	if len(token) != 64 || strings.Trim(token, "0123456789abcdef") != "" {
		t.Fatalf("token should be 64 hex characters, got %q", token)
	}
}
