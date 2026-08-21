package naxbuilder

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSourceTree writes a temporary directory mimicking the NaX layout, with the
// component files already present (so BuildComponents can read them without
// running make).
func fakeSourceTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	dirs := []string{
		"src_loader/bin",
		"src_beacon/build/http",
		"src_sleepmask/dist",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writes := map[string]string{
		"src_loader/bin/nax_loader.x64.bin":       "LOADER",
		"src_beacon/build/http/beacon.x64.bin":    "BEACON",
		"src_beacon/build/http/beacon.pdata.bin":  "PDATA",
		"src_beacon/build/http/beacon.xdata.bin":  "XDATA",
		"src_beacon/build/http/beacon.text_rva":   "2048",
		"src_sleepmask/dist/sleepmask.x64.o":      "SLEEPMASKOBJ",
	}
	for rel, content := range writes {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// withNoMake swaps runMake for a no-op so BuildComponents can operate against the
// pre-written fake tree; it always restores the real runMake on return.
func withNoMake(t *testing.T) {
	t.Helper()
	orig := runMake
	runMake = func(string, string, map[string]string) error { return nil }
	t.Cleanup(func() { runMake = orig })
}

func TestBuildComponentsReadsAllFiles(t *testing.T) {
	withNoMake(t)
	root := fakeSourceTree(t)

	resp, err := BuildComponents(root, &NaxBuildRequest{Transport: "http"})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"loader", "beacon", "pdata", "xdata", "textRva"} {
		if _, ok := resp.Components[name]; !ok {
			t.Errorf("missing component %q", name)
		}
	}
	if string(resp.Components["loader"]) != "LOADER" {
		t.Errorf("loader bytes = %q", string(resp.Components["loader"]))
	}
	if resp.SHA256 == "" || resp.Size == 0 {
		t.Errorf("missing sha256/size: size=%d sha256=%q", resp.Size, resp.SHA256)
	}
	// No stomp flags selected -> flags 0x0000.
	if resp.Flags != "0x0000" {
		t.Errorf("flags = %q, want 0x0000 (no stomp)", resp.Flags)
	}
}

func TestBuildComponentsFlagsFromStomp(t *testing.T) {
	withNoMake(t)
	root := fakeSourceTree(t)

	resp, err := BuildComponents(root, &NaxBuildRequest{
		Transport:  "http",
		StompMode:  true,
		StompUnwind: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// flagModStomp (0x0001) | flagStompPdat (0x0002) = 0x0003.
	if resp.Flags != "0x0003" {
		t.Errorf("flags = %q, want 0x0003", resp.Flags)
	}
}

func TestBuildComponentsMissingComponentErrors(t *testing.T) {
	withNoMake(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src_loader", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src_beacon", "build", "http"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Only loader + beacon are present; pdata/xdata/textRva are absent.
	os.WriteFile(filepath.Join(root, "src_loader", "bin", "nax_loader.x64.bin"), []byte("LOADER"), 0o644)
	os.WriteFile(filepath.Join(root, "src_beacon", "build", "http", "beacon.x64.bin"), []byte("BEACON"), 0o644)

	if _, err := BuildComponents(root, &NaxBuildRequest{Transport: "http"}); err == nil {
		t.Fatal("expected error when a component file is missing")
	}
}

func TestSelectMakeTarget(t *testing.T) {
	cases := []struct {
		req  NaxBuildRequest
		want string
	}{
		{NaxBuildRequest{Transport: "http"}, "components"},
		{NaxBuildRequest{Transport: "http", Debug: true}, "debug-components"},
		{NaxBuildRequest{Transport: "smb"}, "components"},
		{NaxBuildRequest{Transport: "smb", FullRebuild: true}, "link-components"},
		{NaxBuildRequest{Transport: "smb", Debug: true, FullRebuild: true}, "debug-link-components"},
	}
	for _, c := range cases {
		if got := selectMakeTarget(&c.req); got != c.want {
			t.Errorf("selectMakeTarget(%+v) = %q, want %q", c.req, got, c.want)
		}
	}
}
