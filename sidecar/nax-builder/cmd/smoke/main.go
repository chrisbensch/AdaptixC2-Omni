// Command smoke is a manual end-to-end check for the Nax builder sidecar. It
// reads the generated headers that a real server would ship, synthesises the one
// header the pinned tree does not carry (Config_profile.h), and drives a real
// build through the builder's unix socket exactly like the teamserver agent does.
//
// It is a dev-only harness (not part of the builder's runtime path). Run it in an
// environment that has:
//   - NAX_SRC pointing at the pinned NaX source tree (default /nax), and
//   - NAX_BUILDER_SOCK pointing at the builder's socket (default /run/nax/builder.sock).
//
// The client itself only dials the socket (no toolchain needed); the builder does
// the mingw/nasm compile. Build it cross-compiled for Linux and run it in a
// container that shares the builder's /run/nax socket volume.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/entropy-z/AdaptixC2-Omni/sidecar/nax-builder/naxbuilder"
)

const (
	defaultNaxSrc   = "/nax"
	defaultSockPath = "/run/nax/builder.sock"
)

func main() {
	naxSrc := envOr("NAX_SRC", defaultNaxSrc)
	sock := envOr("NAX_BUILDER_SOCK", defaultSockPath)

	cfgDir := filepath.Join(naxSrc, "src_beacon", "include")
	configH, err := os.ReadFile(filepath.Join(cfgDir, "Config.h"))
	if err != nil {
		fail("read Config.h: %v", err)
	}
	sleepmaskH, err := os.ReadFile(filepath.Join(cfgDir, "Config_sleepmask.h"))
	if err != nil {
		fail("read Config_sleepmask.h: %v", err)
	}

	n := profileLen(string(configH))
	profileH := genConfigProfileH(n)

	req := &naxbuilder.NaxBuildRequest{
		Transport:        "http",
		OutputFormat:     "bin",
		ConfigH:          configH,
		ConfigProfileH:   profileH,
		ConfigSleepmaskH: sleepmaskH,
	}

	client := naxbuilder.New(sock)
	resp, err := client.Build(req)
	if err != nil {
		fail("build through %s: %v", sock, err)
	}

	fmt.Printf("[+] build OK\n")
	fmt.Printf("    filename   : %s\n", resp.Filename)
	fmt.Printf("    size       : %d bytes\n", resp.Size)
	fmt.Printf("    sha256     : %s\n", resp.SHA256)
	fmt.Printf("    flags      : %s\n", resp.Flags)
	fmt.Printf("    components :")
	names := make([]string, 0, len(resp.Components))
	for k := range resp.Components {
		names = append(names, k)
	}
	for _, n := range sortedNames(names) {
		fmt.Printf(" %s(%d)", n, len(resp.Components[n]))
	}
	fmt.Println()
}

// profileLen extracts the NAX_PROFILE_LEN value from a generated Config.h (e.g.
// "#define NAX_PROFILE_LEN  1697u"). Falls back to 1697 if it can't be parsed,
// matching the pinned tree's own value.
func profileLen(configH string) int {
	re := regexp.MustCompile(`#define\s+NAX_PROFILE_LEN\s+(\d+)u`)
	if m := re.FindStringSubmatch(configH); m != nil {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		return n
	}
	return 1697
}

// genConfigProfileH renders a syntactically valid Config_profile.h whose
// NAX_PROFILE_WRITE macro writes exactly n bytes, so the beacon compiles against
// it. The byte values are intentionally trivial (0x00) — this harness only needs
// the header to compile, not to decode into a live C2 callback.
func genConfigProfileH(n int) []byte {
	var b strings.Builder
	// Matches the server's writeBytesWriteMacro (perLine=8, fieldWidth=3) exactly,
	// so the beacon's NAX_PROFILE_WRITE(pp) invocation compiles as it would with a
	// real profile. Byte values are 0x00 — this harness only needs the header to
	// compile, not to decode into a live C2 callback.
	b.WriteString("#define NAX_PROFILE_WRITE( p ) do { \\\n")
	for i := 0; i < n; i++ {
		if i%8 == 0 {
			b.WriteString("    ")
		}
		fmt.Fprintf(&b, "(p)[%3d]=0x%02X; ", i, 0)
		if i%8 == 7 {
			b.WriteString("\\\n")
		}
	}
	if n%8 != 0 {
		b.WriteString("\\\n")
	}
	b.WriteString("} while(0)\n")
	return []byte(b.String())
}

func sortedNames(names []string) []string {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	return names
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[!] "+format+"\n", args...)
	os.Exit(1)
}
