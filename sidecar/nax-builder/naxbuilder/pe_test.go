package naxbuilder

import (
	"strings"
	"testing"
)

func TestGenerateShellcodeH(t *testing.T) {
	out := string(generateShellcodeH([]byte{0xde, 0xad, 0x00, 0xff}))

	if !strings.Contains(out, `section(".text")`) {
		t.Fatalf("missing section macro:\n%s", out)
	}
	// The plan's sketch checked for the concatenation "0xdead", but generateShellcodeH
	// emits each byte as its own 0xNN token ("0xde 0xad 0x00 0xff"), so check the
	// individual literals that actually appear.
	for _, lit := range []string{"0xde", "0xad", "0x00", "0xff"} {
		if !strings.Contains(out, lit) {
			t.Fatalf("missing byte literal %s:\n%s", lit, out)
		}
	}
}

// TestCompileWrapperToolchainAbsent verifies the guarded error path: when the
// mingw compiler cannot be found on PATH, compileWrapper returns an error (it is
// checked before any compilation). With a bogus PATH, the toolchain lookup fails
// regardless of whether it is installed elsewhere on the host.
func TestCompileWrapperToolchainAbsent(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-xyz")

	_, _, err := compileWrapper([]byte{0x01}, "exe", "NaxService", "Runner", false, nil)
	if err == nil {
		t.Fatal("expected error when mingw compiler is absent")
	}
}
