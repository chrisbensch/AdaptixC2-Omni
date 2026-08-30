package kharonbuilder

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withNoMake swaps runMake for a no-op so build() can operate against a pre-written
// fake tree; it always restores the real runMake on return.
func withNoMake(t *testing.T) {
	t.Helper()
	orig := runMake
	runMake = func(string, string, []string) (string, error) { return "", nil }
	t.Cleanup(func() { runMake = orig })
}

// withFakeCompile swaps compileWrapper for a stub that returns canned bytes; it
// always restores the real compileWrapper on return.
func withFakeCompile(t *testing.T, payload []byte, name string) {
	t.Helper()
	orig := compileWrapper
	compileWrapper = func(*KharonBuildRequest, []byte) ([]byte, string, error) {
		return payload, name, nil
	}
	t.Cleanup(func() { compileWrapper = orig })
}

// fakeBeaconTree writes a temporary Kharon layout with a beacon .bin already
// present (so build() can read it without running make).
func fakeBeaconTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "src_beacon", "Bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "Kharon.x64.bin"), []byte("BEACON"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBuildBin(t *testing.T) {
	withNoMake(t)
	root := fakeBeaconTree(t)
	SetKharonRoot(root)

	resp, err := build(&KharonBuildRequest{Target: "x64", OutputFormat: "bin"})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Payload) != "BEACON" {
		t.Errorf("payload = %q, want BEACON", string(resp.Payload))
	}
	if resp.Filename != "Kharon.x64.bin" {
		t.Errorf("filename = %q, want Kharon.x64.bin", resp.Filename)
	}
	if !resp.OK {
		t.Errorf("expected OK")
	}
	if resp.Size == 0 || resp.SHA256 == "" {
		t.Errorf("missing size/sha256")
	}
}

func TestBuildBinX86(t *testing.T) {
	withNoMake(t)
	root := t.TempDir()
	binDir := filepath.Join(root, "src_beacon", "Bin")
	os.MkdirAll(binDir, 0o755)
	os.WriteFile(filepath.Join(binDir, "Kharon.x86.bin"), []byte("X86BEACON"), 0o644)
	SetKharonRoot(root)

	resp, err := build(&KharonBuildRequest{Target: "x86", OutputFormat: "bin"})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Payload) != "X86BEACON" {
		t.Errorf("payload = %q, want X86BEACON", string(resp.Payload))
	}
}

func TestBuildLoader(t *testing.T) {
	withNoMake(t)
	withFakeCompile(t, []byte("PE"), "Kharon.x64.exe")
	root := fakeBeaconTree(t)
	SetKharonRoot(root)

	resp, err := build(&KharonBuildRequest{Target: "x64", OutputFormat: "exe"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Filename != "Kharon.x64.exe" {
		t.Errorf("filename = %q, want Kharon.x64.exe", resp.Filename)
	}
	if string(resp.Payload) != "PE" {
		t.Errorf("payload = %q, want PE", string(resp.Payload))
	}
}

func TestBuildMissingBeaconErrors(t *testing.T) {
	withNoMake(t)
	root := t.TempDir()
	SetKharonRoot(root)

	if _, err := build(&KharonBuildRequest{Target: "x64", OutputFormat: "bin"}); err == nil {
		t.Fatal("expected error when beacon .bin is missing")
	}
}

func TestBuildMakeFailurePropagatesLogs(t *testing.T) {
	orig := runMake
	runMake = func(string, string, []string) (string, error) {
		return "boom: out of memory\n", errFakeMake
	}
	t.Cleanup(func() { runMake = orig })
	root := fakeBeaconTree(t)
	SetKharonRoot(root)

	_, err := build(&KharonBuildRequest{Target: "x64", OutputFormat: "bin"})
	if err == nil {
		t.Fatal("expected error when make fails")
	}
	// The error should carry the build logs.
	if !strings.Contains(err.Error(), "boom: out of memory") {
		t.Errorf("error missing build logs: %v", err)
	}
}

var errFakeMake = errors.New("fake make failure")
