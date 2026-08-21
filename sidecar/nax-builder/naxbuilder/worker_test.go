package naxbuilder

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
	return filepath.Join(os.TempDir(), fmt.Sprintf("nax-scar-%s.sock", tag))
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

// TestWorkerRejectsBadTransport starts a listener whose handler validates the
// request; an out-of-allowlist Transport must produce a clear error to the client
// without needing a source tree (validation happens before any build).
func TestWorkerRejectsBadTransport(t *testing.T) {
	sock := shortSockPath("badtransport")
	defer os.Remove(sock)
	go StartListener(sock) // handler: validates (bogus transport -> error) + echoes
	waitForSocket(t, sock)

	c := New(sock)
	resp, err := c.Build(&NaxBuildRequest{Transport: "bogus", OutputFormat: "bin"})
	if err == nil || (resp != nil && resp.OK) {
		t.Fatal("expected error for out-of-allowlist transport")
	}
}

// TestWorkerBuilderAbsent asserts that dialing a path with no listener yields a
// clear error (the connection is refused).
func TestWorkerBuilderAbsent(t *testing.T) {
	sock := shortSockPath("absent") // nothing listening here
	defer os.Remove(sock)
	c := New(sock)
	if _, err := c.Build(&NaxBuildRequest{Transport: "http", OutputFormat: "bin"}); err == nil {
		t.Fatal("expected error when builder is absent")
	}
}

// TestResolveSocketPath checks the socket-path convention: absent file -> fallback;
// present file -> its trimmed, absolutized contents.
func TestResolveSocketPath(t *testing.T) {
	dir := t.TempDir()

	if got := ResolveSocketPath(dir); got != "/run/nax/builder.sock" {
		t.Errorf("absent file fallback = %q, want /run/nax/builder.sock", got)
	}

	target := filepath.Join(dir, "nax_builder_socket")
	if err := os.WriteFile(target, []byte("  /tmp/custom.sock  "), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveSocketPath(dir); got != "/tmp/custom.sock" {
		t.Errorf("with file = %q, want /tmp/custom.sock", got)
	}
}
