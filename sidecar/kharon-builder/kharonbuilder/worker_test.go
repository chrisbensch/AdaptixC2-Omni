package kharonbuilder

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortSockPath returns a short, unique socket path under /tmp. Absolute unix
// socket paths are limited to ~108 bytes (SUN_LEN); t.TempDir()'s /var/folders
// prefix is long, so tests must use a short path.
func shortSockPath(tag string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("kharon-scar-%s.sock", tag))
}

// waitForSocket polls a Unix socket path until it accepts a connection (or times
// out), so a test can be sure a StartListener goroutine has bound its listener.
func waitForSocket(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener on %q never started", sock)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWorkerRejectsBadTarget starts a listener whose handler validates the
// request; an out-of-allowlist Target must produce a clear error to the client
// without needing a source tree (validation happens before any build).
func TestWorkerRejectsBadTarget(t *testing.T) {
	sock := shortSockPath("badtarget")
	defer os.Remove(sock)
	go StartListener(sock) // handler: validates (bogus target -> error) + echoes
	waitForSocket(t, sock)

	c := New(sock)
	resp, err := c.Build(&KharonBuildRequest{Target: "bogus", OutputFormat: "bin"})
	if err == nil || (resp != nil && resp.OK) {
		t.Fatal("expected error for out-of-allowlist target")
	}
}

// TestWorkerBuilderAbsent asserts that dialing a path with no listener yields a
// clear error (the connection is refused).
func TestWorkerBuilderAbsent(t *testing.T) {
	sock := shortSockPath("absent") // nothing listening here
	defer os.Remove(sock)
	c := New(sock)
	if _, err := c.Build(&KharonBuildRequest{Target: "x64", OutputFormat: "bin"}); err == nil {
		t.Fatal("expected error when builder is absent")
	}
}

// TestResolveSocketPath checks the socket-path convention: absent file -> fallback;
// present file -> its trimmed, absolutized contents.
func TestResolveSocketPath(t *testing.T) {
	dir := t.TempDir()

	if got := ResolveSocketPath(dir); got != "/run/kharon/builder.sock" {
		t.Errorf("absent file fallback = %q, want /run/kharon/builder.sock", got)
	}

	target := filepath.Join(dir, "kharon_builder_socket")
	if err := os.WriteFile(target, []byte("  /tmp/custom.sock  "), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveSocketPath(dir); got != "/tmp/custom.sock" {
		t.Errorf("with file = %q, want /tmp/custom.sock", got)
	}
}
