// Command smoke is a manual end-to-end check for the Kharon builder sidecar. It
// drives a real beacon build through the builder's unix socket exactly like the
// teamserver agent does — but the client itself only dials the socket (no
// toolchain needed); the builder image does the mingw/nasm/objcopy compile.
//
// It is a dev-only harness (not part of the builder's runtime path). Run it in an
// environment that has:
//   - KHARON_SRC pointing at the pinned Kharon source tree (default /app/kharon),
//   - KHARON_BUILDER_SOCK pointing at the builder's socket (default
//     /run/kharon/builder.sock).
//
// Build it cross-compiled for Linux and run it in a container that shares the
// builder's /run/kharon socket volume (and, for the loader-PE check, the /app/kharon
// source tree).
package main

import (
	"fmt"
	"os"

	"github.com/entropy-z/AdaptixC2-Omni/sidecar/kharon-builder/kharonbuilder"
)

const (
	defaultKharonSrc   = "/app/kharon"
	defaultSockPath    = "/run/kharon/builder.sock"
	// defaultMalleableHex is a minimal Cobalt-Strike-style malleable C2 profile
	// (one HTTP_GET callback). The beacon's Config.cc needs a non-empty
	// HTTP_MALLEABLE_BYTES to compile, so the smoke test ships this fallback.
	defaultMalleableHex = "{ 0x61, 0x74, 0x74, 0x72, 0x69, 0x62, 0x75, 0x74, 0x65, 0x20, 0x7b, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x48, 0x54, 0x54, 0x50, 0x5f, 0x47, 0x45, 0x54, 0x20, 0x7b, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x49, 0x64, 0x65, 0x6e, 0x74, 0x69, 0x66, 0x69, 0x65, 0x72, 0x20, 0x22, 0x2f, 0x64, 0x65, 0x66, 0x61, 0x75, 0x6c, 0x74, 0x2e, 0x61, 0x73, 0x70, 0x78, 0x22, 0x3b, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x7d, 0x0a, 0x7d, 0x0a, 0x0a, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x20, 0x7b, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x48, 0x54, 0x54, 0x50, 0x5f, 0x47, 0x45, 0x54, 0x20, 0x7b, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x49, 0x64, 0x65, 0x6e, 0x74, 0x69, 0x66, 0x69, 0x65, 0x72, 0x20, 0x22, 0x2f, 0x64, 0x65, 0x66, 0x61, 0x75, 0x6c, 0x74, 0x2e, 0x61, 0x73, 0x70, 0x78, 0x22, 0x3b, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x7d, 0x0a, 0x7d, 0x0a }"
)

func main() {
	_ = envOr("KHARON_SRC", defaultKharonSrc)
	sock := envOr("KHARON_BUILDER_SOCK", defaultSockPath)

	// 1) Raw PIC beacon (bin) — proves the beacon make + objcopy path.
	binReq := &kharonbuilder.KharonBuildRequest{
		Target:           "x64",
		OutputFormat:     "bin",
		KhAgentUUID:      "smoke-0001",
		KhSleepTime:      "5",
		KhJitter:         20,
		KhSleepMask:      0,
		KhHeapMask:       false,
		KhSyscall:        0,
		KhAmsiEtwBypass:  0,
		KhBofHookEnabled: false,
		HTTPMalleableHex: defaultMalleableHex,
	}
	client := kharonbuilder.New(sock)
	resp, err := client.Build(binReq)
	if err != nil {
		fail("beacon (bin) build through %s: %v", sock, err)
	}
	report("beacon (bin)", resp)

	// 2) Loader PE (exe) — proves the Shellcode.h write + clang++ wrapper path.
	exeReq := &kharonbuilder.KharonBuildRequest{
		Target:           "x64",
		OutputFormat:     "exe",
		KhAgentUUID:      "smoke-0002",
		KhSleepTime:      "5",
		KhJitter:         20,
		KhSleepMask:      0,
		KhHeapMask:       false,
		KhSyscall:        0,
		KhAmsiEtwBypass:  0,
		KhBofHookEnabled: false,
		HTTPMalleableHex: defaultMalleableHex,
	}
	exeResp, err := client.Build(exeReq)
	if err != nil {
		fail("loader (exe) build through %s: %v", sock, err)
	}
	report("loader (exe)", exeResp)

	fmt.Printf("[+] all kharon builder checks passed\n")
}

func report(kind string, resp *kharonbuilder.KharonBuildResponse) {
	fmt.Printf("[+] %s build OK\n", kind)
	fmt.Printf("    filename   : %s\n", resp.Filename)
	fmt.Printf("    size       : %d bytes\n", resp.Size)
	fmt.Printf("    sha256     : %s\n", resp.SHA256)
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
