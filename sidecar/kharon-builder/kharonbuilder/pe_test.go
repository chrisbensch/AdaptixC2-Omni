package kharonbuilder

import (
	"strings"
	"testing"
)

func TestGenerateShellcodeH(t *testing.T) {
	out := generateKharonShellcodeH([]byte{0xde, 0xad, 0x00, 0xff})

	if !strings.Contains(out, `section(".text")`) {
		t.Fatalf("missing section macro:\n%s", out)
	}
	if !strings.Contains(out, "constexpr size_t Size = 4;") {
		t.Fatalf("missing Size constant:\n%s", out)
	}
	// generateKharonShellcodeH emits each byte as its own 0xNN token
	// ("0xde 0xad 0x00 0xff"), so check the individual literals that appear.
	for _, lit := range []string{"0xde", "0xad", "0x00", "0xff"} {
		if !strings.Contains(out, lit) {
			t.Fatalf("missing byte literal %s:\n%s", lit, out)
		}
	}
}

// TestCompileWrapperToolchainAbsent verifies the guarded error path: when clang++
// cannot be found on PATH, compileWrapper returns an error (it is checked before
// any compilation). With a bogus PATH, the toolchain lookup fails regardless of
// whether it is installed elsewhere on the host.
func TestCompileWrapperToolchainAbsent(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-xyz")

	_, _, err := compileWrapper(&KharonBuildRequest{OutputFormat: "exe"}, []byte{0x01})
	if err == nil {
		t.Fatal("expected error when clang++ is absent")
	}
}
