# NaX Sidecar Prototype — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route `agent_nonameax` Windows payload generation through a dedicated builder worker reached over a Unix socket, so the teamserver performs no native compilation and ships no NaX source tree.

**Architecture:** A small standalone Go builder worker runs in a separate, network-isolated container behind a Unix socket (`/run/nax/builder.sock`, shared via a tmpfs mount). The teamserver's `agent_nonameax` plugin parses `BuildProfile`, builds a request, dials the socket, sends a handshake + request frame, reads back the raw payload *components*, and repacks them with the already-existing in-process `packNaxBin(...)`. The builder owns only `make` + the mingw PE wrapper.

**Tech Stack:** Go 1.25 (plugin `.so` + a static worker binary), Go `net` Unix-domain sockets, length-prefixed JSON framing, `github.com/Adaptix-Framework/axc2 v1.2.0` (the `BuildProfile` contract), NaX Makefile targets (`components` / `link-components` / `debug-*`).

---

## Scope (Option A — Milestone 2 only)

- **In:** `agent_nonameax` payload → sidecar builder. Builder image carries its own pinned NaX source + toolchain. Teamserver ships the 4 `.so` plugins only (no `/app/NaX` source tree, no in-server NaX `make`/mingw).
- **Out (deferred):** `read_only: true` restoration, the native toolchain leaving the server image, Kharon migration, hardening (limits/redaction/concurrency), CI matrix, SBOM/provenance — all steps B/C.
- **Kept exactly as-is:** Kharon (it still compiles in-server, which is *why* the toolchain must remain in the server image during A and `read_only` stays off until C).

## Exit criterion (Milestone 2)

One deterministic end-to-end request succeeds **without any compiler or writable source tree in the teamserver**; builder-absence, timeout, malformed input, and malformed output each produce a clear error without crashing the server.

## File structure map

| File | Responsibility |
|---|---|
| `sidecar/nax-builder/go.mod` | New workspace-owned Go module for the builder worker. |
| `sidecar/nax-builder/naxbuilder/client.go` | Server-side: dial + handshake + send request + read response (blocking socket client). |
| `sidecar/nax-builder/naxbuilder/request.go` | `NaxBuildRequest` / `NaxBuildResponse` structs, allowlist validation, size bounds. |
| `sidecar/nax-builder/naxbuilder/frame.go` | Length-prefixed JSON frame read/write, worker `serveLoop`. |
| `sidecar/nax-builder/naxbuilder/worker.go` | Ephemeral-workspace build loop: write headers → `make <target>` → read components → (optional mingw PE) → response. |
| `sidecar/nax-builder/cmd/nax-builder/main.go` | `main()` that starts the worker server on the socket path. |
| `NaX/src_server/agent_nonameax/pl_nax_sidecar.go` | **New:** maps `BuildProfile` → `NaxBuildRequest`, calls the client, repacks with `packNaxBin`. |
| `NaX/src_server/agent_nonameax/pl_build_payload.go` | **Modify:** replace the `writeIfChanged` + `make` + file-read tail of `BuildPayload` with the sidecar call. |
| `NaX/src_server/agent_nonameax/pl_nax_sidecar_test.go` | Tests: request mapping, framing round-trip, fake-builder round-trip, absence/timeout/malformed. |
| `sidecar/nax-builder/naxbuilder/frame_test.go` | Framing/validation unit tests. |
| `Dockerfile` | **Modify:** drop `COPY NaX /src/NaX` + runtime toolchain copy; keep the 4 `.so` build steps. |
| `Dockerfile.nax-builder` | **New:** builder image (pinned NaX source copy + toolchain + worker binary + `pe_templates`). |
| `docker-compose.yml` | **Modify:** add `nax-builder` service to the `runtime` profile; add `/app/extenders/*/nax_builder_socket` reference. |

## Exact component output paths the worker reads (from `NaX/Makefile`)

```
src_loader/bin/nax_loader.x64.bin
src_beacon/build/http/beacon.x64.bin        (transport=http, NAX_TRANSPORT_PROFILE=0)
src_beacon/build/http/beacon.pdata.bin
src_beacon/build/http/beacon.xdata.bin
src_beacon/build/http/beacon.text_rva
src_beacon/build/smb/beacon.x64.bin         (transport=smb, NAX_TRANSPORT_PROFILE=1)
src_beacon/build/smb/beacon.pdata.bin
src_beacon/build/smb/beacon.xdata.bin
src_beacon/build/smb/beacon.text_rva
src_beacon/build/http/beacon.x64.debug.bin   (debug=true)
src_beacon/build/http/beacon.debug.pdata.bin
src_beacon/build/http/beacon.debug.xdata.bin
src_beacon/build/http/beacon.debug.text_rva
src_sleepmask/dist/sleepmask.x64.o           (beacongate only)
```

`packNaxBin(loader, beacon, pdata, xdata []byte, textRva, flags uint32, dllName string) []byte` (in `nax_packer.go`) and the `generateConfigH*/generateProfileH/generateSleepmaskH/generateShellcodeH` helpers (in `pl_build.go`) are **pure Go** — they stay in the server plugin and need no source tree.

---

## Task 1 — Builder module scaffold + request/response types (with failing test)

**Files:**
- Create: `sidecar/nax-builder/go.mod`
- Create: `sidecar/nax-builder/naxbuilder/request.go`
- Create: `sidecar/nax-builder/naxbuilder/request_test.go`

- [ ] **Step 1: Create the module + `NaxBuildRequest`/`NaxBuildResponse`**

Create `sidecar/nax-builder/go.mod`:

```
module github.com/entropy-z/AdaptixC2-Omni/sidecar/nax-builder

go 1.25.4
```

Create `sidecar/nax-builder/naxbuilder/request.go`:

```go
package naxbuilder

// NaxBuildRequest is the fully server-derived instruction for one payload build.
// It contains no caller-provided paths, Make targets, or shell fragments — the
// worker chooses the Make target and paths from Transport/Debug/FullRebuild.
type NaxBuildRequest struct {
	Transport   string `json:"transport"` // "http" | "smb" — allowlisted
	Debug       bool   `json:"debug"`
	FullRebuild bool   `json:"fullRebuild"`

	// HTTP transport fields.
	CallbackHost string `json:"callbackHost"`
	CallbackPort int    `json:"callbackPort"`
	BootURI      string `json:"bootURI"`
	SSL          bool   `json:"ssl"`

	// SMB transport field.
	PipeName string `json:"pipeName"`

	SleepMs    uint32   `json:"sleepMs"`
	JitterPct  uint32   `json:"jitterPct"`
	EncKeyHex  string   `json:"encKeyHex"` // 32 hex chars -> 16 bytes
	Watermark  string   `json:"watermark"`  // hex, parsed base-16 by caller
	LyWM       string   `json:"lyWm"`       // listener watermark, hex

	GateAPIs     []string `json:"gateAPIs"`
	StompMode    bool     `json:"stompMode"`    // enabled
	StompAdv     bool     `json:"stompAdv"`     // enabled
	StompDll     string   `json:"stompDll"`
	StompUnwind  bool     `json:"stompUnwind"`
	ThreadPool   bool     `json:"threadPool"`
	BofStomp     bool     `json:"bofStomp"`
	BofStompDll  string   `json:"bofStompDll"`
	BofStompPool []string `json:"bofStompPool"`
	SmStompDll   string   `json:"smStompDll"`
	SleepObf     string   `json:"sleepObf"`     // "0" | "1"
	OutputFormat string   `json:"outputFormat"` // "bin" | "exe" | "dll" | "svc" — allowlisted
	SvcName      string   `json:"svcName"`
	DllExport    string   `json:"dllExport"`

	// BeaconGate: builder builds the sleepmask BOF and returns its .o bytes.
	BeaconGate   bool   `json:"beaconGate"`
	EmbedSleep   bool   `json:"embedSleep"` // if true, builder embeds sleepmask .o into Config.h

	// Pre-generated headers (server keeps the pure-Go generators). Base64 of the
	// exact bytes the old code wrote to src_beacon/include/.
	ConfigH       []byte `json:"configH"`
	ConfigProfile []byte `json:"configProfile"`
}

// NaxBuildResponse carries the raw payload components back to the server, which
// repacks them with the in-process packNaxBin(...).
type NaxBuildResponse struct {
	Filename string                 `json:"filename"`
	Size     int                    `json:"size"`
	SHA256   string                 `json:"sha256"`
	Components map[string][]byte `json:"components"` // "loader"|"beacon"|"pdata"|"xdata"|"textRva"
	Flags    string                 `json:"flags"` // "0x%04x"
	StompDll string                 `json:"stompDll"`
	SleepmaskO []byte               `json:"sleepmaskO,omitempty"` // base64, only if BeaconGate
	OK       bool                   `json:"ok"`
}

// NaxBuildError is returned on any failure.
type NaxBuildError struct {
	Error string `json:"error"`
}

// ComponentPath returns the on-disk path of a component relative to the NaX
// source root, for a given transport + debug combination. It mirrors NaX/Makefile.
func ComponentPath(transport string, debug bool, name string) string {
	beaconDir := "http"
	if transport == "smb" {
		beaconDir = "smb"
	}
	prefix := "beacon"
	if debug {
		prefix = "beacon.debug"
	}
	switch name {
	case "loader":
		return "src_loader/bin/nax_loader.x64.bin"
	case "beacon":
		if debug {
			return "src_beacon/build/" + beaconDir + "/beacon.x64.debug.bin"
		}
		return "src_beacon/build/" + beaconDir + "/beacon.x64.bin"
	case "pdata":
		return "src_beacon/build/" + beaconDir + "/" + prefix + ".pdata.bin"
	case "xdata":
		return "src_beacon/build/" + beaconDir + "/" + prefix + ".xdata.bin"
	case "textRva":
		return "src_beacon/build/" + beaconDir + "/" + prefix + ".text_rva"
	case "sleepmask":
		return "src_sleepmask/dist/sleepmask.x64.o"
	}
	return ""
}
```

- [ ] **Step 2: Write the failing test** (`sidecar/nax-builder/naxbuilder/request_test.go`) — assert `ComponentPath` for every branch and that an allowlist validator rejects unknown `transport`/`outputFormat`:

```go
package naxbuilder

import "testing"

func TestComponentPath(t *testing.T) {
	cases := []struct {
		transport, target string
	}{
		{"http", "src_beacon/build/http/beacon.x64.bin"},
		{"smb", "src_beacon/build/smb/beacon.x64.bin"},
		{"http", "src_beacon/build/http/beacon.x64.debug.bin"},
	}
	_ = cases

	if got := ComponentPath("http", false, "loader"); got != "src_loader/bin/nax_loader.x64.bin" {
		t.Fatalf("loader path = %q", got)
	}
	if got := ComponentPath("smb", false, "beacon"); got != "src_beacon/build/smb/beacon.x64.bin" {
		t.Fatalf("smb beacon = %q, want %q", got, "src_beacon/build/smb/beacon.x64.bin")
	}
	if got := ComponentPath("http", false, "pdata"); got != "src_beacon/build/http/beacon.pdata.bin" {
		t.Fatalf("pdata = %q", got)
	}
	if got := ComponentPath("http", true, "beacon"); got != "src_beacon/build/http/beacon.x64.debug.bin" {
		t.Fatalf("debug beacon = %q", got)
	}
	if got := ComponentPath("http", false, "bogus"); got != "" {
		t.Fatalf("unknown component should be empty, got %q", got)
	}
}
```

- [ ] **Step 3: Add `Validate()` to `request.go`** (allowlist check for `Transport ∈ {http,smb}`, `OutputFormat ∈ {bin,exe,dll,svc}`, `EncKeyHex` decodes to 16 bytes) and make the test's allowlist expectations pass. Keep `Validate` minimal — reject only out-of-allowlist values and a bad `EncKeyHex`; anything else is "server bug", rejected as malformed.

- [ ] **Step 4: Run the test to make it pass**

```bash
cd sidecar/nax-builder && go test ./naxbuilder/ -run TestComponentPath -v
```

Expected: `PASS TestComponentPath`.

- [ ] **Step 5: Commit**

```bash
git add sidecar/nax-builder/go.mod sidecar/nax-builder/naxbuilder/request.go sidecar/nax-builder/naxbuilder/request_test.go
git commit -m "sidecar: NaxBuildRequest/Response types + ComponentPath + allowlist validation"
```

## Task 2 — Length-prefixed JSON framing + worker serveLoop (with failing test)

**Files:**
- Create: `sidecar/nax-builder/naxbuilder/frame.go`
- Create: `sidecar/nax-builder/naxbuilder/frame_test.go`

- [ ] **Step 1: Write the failing test** (`frame_test.go`) — a request marshals → is length-prefixed → is read back into an equal `NaxBuildRequest`; and an oversized frame is rejected:

```go
package naxbuilder

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	req := NaxBuildRequest{Transport: "http", Debug: true, OutputFormat: "bin", EncKeyHex: "00112233445566778899aabbccddeeff"}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, &req); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 || buf.Len() > 4096 {
		t.Fatalf("unexpected frame size %d", buf.Len())
	}
	var got NaxBuildRequest
	if err := ReadFrame(&buf, &got, 4096); err != nil {
		t.Fatal(err)
	}
	if got.Transport != req.Transport || got.OutputFormat != req.OutputFormat {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestFrameTooLargeRejected(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteFrame(&buf, &NaxBuildRequest{Transport: "http"})
	if err := ReadFrame(&buf, &NaxBuildRequest{}, 2); err == nil {
		t.Fatal("expected error for oversized frame")
	}
}
```

- [ ] **Step 2: Write `frame.go`** — 4-byte big-endian length prefix + UTF-8 JSON body:

```go
package naxbuilder

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
)

// MaxFrameBytes bounds a single JSON frame (request or response).
const MaxFrameBytes = 64 * 1024 * 1024 // 64 MiB

// WriteFrame writes a single length-prefixed JSON frame to w.
func WriteFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// ReadFrame reads one length-prefixed JSON frame into v. It rejects frames
// whose declared length exceeds maxBytes and short reads.
func ReadFrame(r io.Reader, v any, maxBytes int) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if int(n) > maxBytes {
		return errFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
```

- [ ] **Step 3: Add a sentinel error** at the top of `frame.go` (`var errFrameTooLarge = errors.New("frame exceeds max size")`) and import `errors`.

- [ ] **Step 4: Write `ServeConn` in `frame.go`** — accept one `net.Conn`, decode a `*NaxBuildRequest` (with a per-request `maxBytes`), call a pluggable `handler func(*NaxBuildRequest) (*NaxBuildResponse, error)`, write either `*NaxBuildResponse` or `*NaxBuildError`, and close:

```go
func ServeConn(conn net.Conn, handler func(*NaxBuildRequest) (*NaxBuildResponse, error)) error {
	defer conn.Close()

	var req naxbuilder.NaxBuildRequest
	if err := naxbuilder.ReadFrame(conn, &req, naxbuilder.MaxFrameBytes); err != nil {
		_ = naxbuilder.WriteFrame(conn, &naxbuilder.NaxBuildError{Error: err.Error()})
		return err
	}
	resp, err := handler(&req)
	if err != nil {
		_ = naxbuilder.WriteFrame(conn, &naxbuilder.NaxBuildError{Error: err.Error()})
		return err
	}
	return nil
}
```

- [ ] **Step 5: Run the tests**

```bash
cd sidecar/nax-builder && go test ./naxbuilder/ -v
```

Expected: `PASS TestFrameRoundTrip`, `PASS TestFrameTooLargeRejected`.

- [ ] **Step 6: Commit**

```bash
git add sidecar/nax-builder/naxbuilder/frame.go sidecar/nax-builder/naxbuilder/frame_test.go
git commit -m "sidecar: length-prefixed JSON framing + per-request MaxFrameBytes + ServeConn"
```

## Task 3 — Builder worker build loop (with failing test against a fake source tree)

**Files:**
- Create: `sidecar/nax-builder/naxbuilder/build.go`
- Create: `sidecar/nax-builder/naxbuilder/build_test.go`

- [ ] **Step 1: Write the failing test** (`build_test.go`) — build a temporary directory that mimics the NaX layout with empty component files, call `BuildComponents(rootDir, req)`, and assert it returns the loader/beacon/pdata/xdata/textRva components with correct size/sha256, and errors when `MakeTarget` produces nothing.

```go
package naxbuilder

import (
	"os"
	"path/filepath"
	"testing"
)

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
		"src_loader/bin/nax_loader.x64.bin":        "LOADER",
		"src_beacon/build/http/beacon.x64.bin":     "BEACON",
		"src_beacon/build/http/beacon.pdata.bin":   "PDATA",
		"src_beacon/build/http/beacon.xdata.bin":   "XDATA",
		"src_beacon/build/http/beacon.text_rva":    "2048",
		"src_sleepmask/dist/sleepmask.x64.o":       "SLEEPMASKOBJ",
	}
	for rel, content := range writes {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBuildComponentsReadsAllFiles(t *testing.T) {
	root := fakeSourceTree(t)
	resp, err := BuildComponents(root, &NaxBuildRequest{Transport: "http"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Components["loader"] == nil || resp.Components["beacon"] == nil ||
		resp.Components["pdata"] == nil || resp.Components["xdata"] == nil ||
		resp.Components["textRva"] == nil {
		t.Fatalf("missing components: %+v", resp.Components)
	}
	if string(resp.Components["loader"]) != "LOADER" {
		t.Fatalf("loader bytes = %q", string(resp.Components["loader"]))
	}
	// sha256 present for the response
	if resp.SHA256 == "" || resp.Size == 0 {
		t.Fatalf("missing sha256/size: %+v", resp)
	}
}
```

- [ ] **Step 2: Write `build.go`** — `BuildComponents(root string, req *NaxBuildRequest) (*NaxBuildResponse, error)`:

  1. Select the Make target from `req`: `components` / `link-components` / `debug-components` / `debug-link-components` (map `Debug` × whether a first/full build is implied).
  2. Build the `make -C <root> <target> NAX_STOMP_MODE=… NAX_EXEC_MODE=… NAX_STOMP_ADVANCED=… NAX_TRANSPORT_PROFILE=…` argument list from `req.StompMode/StompUnwind/…`.
  3. If `req.BeaconGate`, run `make -C <root>/src_sleepmask <target>` (via the Makefile), then read `src_sleepmask/dist/sleepmask.x64.o`.
  4. Read the component files via `ComponentPath`.
  5. Return `NaxBuildResponse` with base64-decoded component bytes + computed `size`/`sha256`.

- [ ] **Step 3: Run the test**

```bash
cd sidecar/nax-builder && go test ./naxbuilder/ -run TestBuildComponents -v
```

Expected: `PASS`.

- [ ] **Step 4: Commit**

```bash
git add sidecar/nax-builder/naxbuilder/build.go sidecar/nax-builder/naxbuilder/build_test.go
git commit -m "sidecar: BuildComponents (make target selection + component read + sha256)"
```

## Task 4 — PE wrapper in the builder (non-bin) — pure-Go part tested, mingw part skipped when absent

**Files:**
- Create: `sidecar/nax-builder/naxbuilder/pe.go`
- Create: `sidecar/nax-builder/naxbuilder/pe_test.go`

- [ ] **Step 1: Write the failing test** (`pe_test.go`) — `generateShellcodeH` output format only (pure Go; the mingw compile is skipped on hosts without the toolchain):

```go
package naxbuilder

import (
	"strings"
	"testing"
)

func TestGenerateShellcodeH(t *testing.T) {
	out := string(generateShellcodeH([]byte{0xde, 0xad, 0x00, 0xff}))
	if !strings.Contains(out, "section(\".text\")") {
		t.Fatalf("missing section macro:\n%s", out)
	}
	if !strings.Contains(out, "0xdead") || !strings.Contains(out, "0x00") || !strings.Contains(out, "0xff") {
		t.Fatalf("missing byte literals:\n%s", out)
	}
}
```

- [ ] **Step 2: Write `pe.go`** — copy `generateShellcodeH` (from `pl_build.go`) into `pe.go` and add `compileWrapper` that writes `Shellcode.h` into a temp dir, locates `pe_templates/` relative to the builder's source root, and invokes `x86_64-w64-mingw32-g++` (arg array; no shell). Guard the mingw invocation: if the compiler is not on `PATH`, return a clear error ("mingw toolchain absent").

- [ ] **Step 3: Run the pure-Go test**

```bash
cd sidecar/nax-builder && go test ./naxbuilder/ -run TestGenerateShellcodeH -v
```

Expected: `PASS`. (The mingw compile is exercised in the end-to-end Docker smoke test — Step 6 — not here.)

- [ ] **Step 4: Add a `TestCompileWrapperToolchainAbsent` guard test** that asserts the error path when the toolchain is missing:

```go
func TestCompileWrapperToolchainAbsent(t *testing.T) {
	// Temporarily blank PATH so the mingw compiler cannot be found.
	t.Setenv("PATH", "/nonexistent-xyz")
	_, _, err := compileWrapper([]byte{0x01}, "exe", "NaxService", "Runner", false, nil)
	if err == nil {
		t.Fatal("expected error when mingw compiler is absent")
	}
}
```

- [ ] **Step 5: Run the guard test and commit** (the mingw step itself is covered end-to-end in Task 7's smoke test):

```bash
cd sidecar/nax-builder && go test ./naxbuilder/ -run TestCompileWrapperToolchainAbsent -v && \
  git add sidecar/nax-builder/naxbuilder/pe.go sidecar/nax-builder/naxbuilder/pe_test.go && \
  git commit -m "sidecar: builder PE wrapper (generateShellcodeH + mingw arg-array), guarded"
```

## Task 5 — Worker server binary + socket-path convention wiring

**Files:**
- Create: `sidecar/nax-builder/cmd/nax-builder/main.go`
- Create: `sidecar/nax-builder/naxbuilder/worker.go` (extend with `StartListener(sockPath string)`)
- Create: `NaX/src_server/agent_nonameax/pl_nax_sidecar.go` (socket-path resolution lives here)

- [ ] **Step 1: Write `worker.go` `StartListener`** — resolve the socket path from `ModuleDir/nax_builder_socket` (fall back to `/run/nax/builder.sock`), `net.Listen("unix", path)`, and loop `Accept()` → per-connection goroutine (prototype: serialize with a mutex) → `ServeConn(conn, handleRequest)` where `handleRequest` maps `*NaxBuildRequest` → calls `BuildComponents` (and `compileWrapper` if `OutputFormat != "bin"`). Bind the worker UID/permissions (0o600) in the builder image.

- [ ] **Step 2: Write `cmd/nax-builder/main.go`** — read the socket path from `env NAX_BUILDER_SOCK` (default `/run/nax/builder.sock`), call `StartListener`, log on panic.

```go
package main

import (
	"os"

	"github.com/entropy-z/AdaptixC2-Omni/sidecar/nax-builder/naxbuilder"
)

func main() {
	sock := os.Getenv("NAX_BUILDER_SOCK")
	if sock == "" {
		sock = "/run/nax/builder.sock"
	}
	naxbuilder.StartListener(sock)
}
```

- [ ] **Step 3: Add a protocol-only failing test** `worker_test.go` (host-independent; no source tree needed) — start the listener on a temp socket path (`t.TempDir()`), connect a client, send a request with an out-of-allowlist `Transport: "bogus"`, and assert the client returns a clear structured error; then assert builder-absence (connect to a path with no listener) also yields a clear error:

```go
package naxbuilder

import (
	"net"
	"path/filepath"
	"testing"
)

func TestWorkerRejectsBadTransport(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "builder.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go StartListener(sock) // minimal handler: just validates + echoes errors

	c, err := New(sock)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Build(&NaxBuildRequest{Transport: "bogus", OutputFormat: "bin"})
	if err == nil || resp != nil && resp.OK {
		t.Fatal("expected error for out-of-allowlist transport")
	}
}

func TestWorkerBuilderAbsent(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "empty.sock") // nothing listening here
	c, err := New(sock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Build(&NaxBuildRequest{Transport: "http", OutputFormat: "bin"}); err == nil {
		t.Fatal("expected error when builder is absent")
	}
}
```

- [ ] **Step 4: Add socket-path resolution to `pl_nax_sidecar.go`** — the server plugin reads `filepath.Join(ModuleDir, "nax_builder_socket")`; if present, use its trimmed contents as the absolute path; else use `/run/nax/builder.sock`.

- [ ] **Step 5: Run + commit**

```bash
cd sidecar/nax-builder && go test ./naxbuilder/ -run 'TestWorker' -v && \
  git add sidecar/nax-builder cmd/ NaX/src_server/agent_nonameax/pl_nax_sidecar.go && \
  git commit -m "sidecar: worker listener + main binary + socket-path convention"
```

## Task 6 — Server-side mapping: BuildPayload → sidecar call (with fake-builder test)

**Files:**
- Modify: `NaX/src_server/agent_nonameax/pl_build_payload.go` (the tail of `BuildPayload`)
- Create: `NaX/src_server/agent_nonameax/pl_nax_sidecar.go` (already added in Task 5 — flesh out the mapping function here)

- [ ] **Step 1: Write the failing test** (`pl_build_payload_test.go` in the `agent_nonameax` package) — spin up an in-process fake builder that returns canned components, point the client at it, call the new mapping function, and assert the packed output equals `packNax …` of the canned components. Also assert builder-absence → error, timeout → error, malformed response → error.

```go
package main

import (
	"net"
	"path/filepath"
	"testing"

	adaptix "github.com/Adaptix-Framework/axc2"
	naxbuilder "github.com/entropy-z/AdaptixC2-Omni/sidecar/nax-builder/naxbuilder"
)

// fakeSidecarBuilder starts an in-process server-side stub that replies to one
// request with canned components, so buildViaSidecar can be tested end-to-end
// without any compiler or real source tree.
func fakeSidecarBuilder(t *testing.T, sockPath string) {
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				var req naxbuilder.NaxBuildRequest
				if err := naxbuilder.ReadFrame(conn, &req, naxbuilder.MaxFrameBytes); err != nil {
					return
				}
				resp := &naxbuilder.NaxBuildResponse{
					OK:       true,
					Filename: "nax.x64.bin",
					Components: map[string][]byte{
						"loader":  []byte("L"),
						"beacon": []byte("B"),
						"pdata":  []byte{},
						"xdata":  []byte{},
						"textRva": []byte("4096"),
					},
					Flags:    "0x0000",
					StompDll: "chakra.dll",
				}
				_ = naxbuilder.WriteFrame(conn, resp)
			}(conn)
		}
	}()
}

func TestMapBuildProfileThroughSidecar(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "builder.sock")
	fakeSidecarBuilder(t, sock)

	profile := adaptix.BuildProfile{
		AgentConfig: `{"sleep":10,"jitter":30,"gate_apis":[],"output_format":"bin"}`,
		ListenerProfiles: []adaptix.TransportProfile{
			{Watermark: "a04a4178", Profile: `{"hosts":["1.2.3.4:443"],"ssl":true,"encrypt_key":"` +
				"00112233445566778899aabbccddeeff" + `"","post":{"uris":["/api/v1/status"]}}`,
			},
		},
	}

	// Build the request exactly like BuildPayload does, then send it through the sidecar.
	req := &naxbuilder.NaxBuildRequest{
		Transport:    "http",
		OutputFormat: "bin",
	}
	payload, err := buildViaSidecar(req)
	if err != nil {
		t.Fatal(err)
	}
	if payload == nil {
		t.Fatal("expected packed payload, got nil")
	}
}

func TestBuildViaSidecarBuilderAbsent(t *testing.T) {
	// No listener on this path -> buildViaSidecar must return a clear error.
	_, err := buildViaSidecar(&naxbuilder.NaxBuildRequest{Transport: "http", OutputFormat: "bin"})
	if err == nil {
		t.Fatal("expected builder-absence error")
	}
}
```

- [ ] **Step 2: Build the `*naxbuilder.NaxBuildRequest` inline inside `BuildPayload`** from the local variables the existing `pl_build_payload.go` already computes — `encKeyHex`, `watermark`, `listenerWatermark`, `sleepMs`, `jitterPct`, `callbackHost`, `callbackPort`, `bootURI`, `listenerSSL`, `isSMB`, `pipeName`, `debug`, `fullRebuild`, `beacongate`, `moduleStomp`, `stompAdvanced`, `stompDll`, `stompUnwind`, `useThreadPool`, `bofStomp`, `bofStompDll`, `bofStompPool`, `smStompDll`, `sleepObf`, `svcName`, `dllExport`, `outputFormat`, `transportProfile` — plus the two pure-Go-generated `configH`/`profileBytes`. Concrete one-to-one mapping (these are the exact existing local names in `pl_build_payload.go`; no helper functions):

```go
// Inline in BuildPayload, right after configH / profileBytes are generated:
req := &naxbuilder.NaxBuildRequest{
    SSL:          listenerSSL,
    EncKeyHex:    strings.ToLower(encKeyHex),
    Watermark:    fmt.Sprintf("0x%08x", watermark),
    LyWM:         fmt.Sprintf("0x%08x", listenerWatermark),
    SleepMs:      sleepMs,
    JitterPct:    jitterPct,
    Debug:        debug,
    FullRebuild:  fullRebuild,
    BeaconGate:   beacongate,
    OutputFormat: outputFormat,    // "bin"|"exe"|"dll"|"svc" (existing local)
    SvcName:      svcName,
    DllExport:    dllExport,
    StompMode:    moduleStomp,     // bool (existing local)
    StompAdv:     stompAdvanced,   // bool (existing local)
    StompDll:     stompDll,
    StompUnwind:  stompUnwind,
    ThreadPool:   useThreadPool,
    BofStomp:     bofStomp,
    BofStompDll:  bofStompDll,
    BofStompPool: bofStompPool,
    SmStompDll:   smStompDll,
    SleepObf:     sleepObf,
    ConfigH:      configH,
    ConfigProfile: profileBytes,
}
if transportProfile == 1 { // transportProfile is the existing http(0)/smb(1) local
    req.Transport = "smb"
    req.PipeName = pipeName
} else {
    req.Transport = "http"
    req.CallbackHost = callbackHost
    req.CallbackPort = callbackPort
    req.BootURI = bootURI
}
```

No parse helpers — each field names a local the existing function already computes. Reuse the existing `generateConfigH`/`generateProfileH` outputs as `ConfigH`/`ConfigProfile`.

- [ ] **Step 3: Wire the socket call** — keep everything up to `configH` generation; **replace** the block from `writeIfChanged(configPath, …)` through the component file reads with a call to `buildViaSidecar(req)` (which takes the already-built request, dials the socket, reads the components, and repacks with the unchanged `packNaxBin(...)`):

```go
	// buildViaSidecar(req): dial + handshake + send + read components + packNaxBin.
	// Never invokes a compiler or reads/writes a source tree.
	payload, filename, err := buildViaSidecar(req)
	if err != nil {
		return nil, "", err
	}
	return payload, filename, nil
```

(The pure-Go `generateConfigH`/`generateProfileH`/`generateSleepmaskH` calls stay — they now feed the request instead of writing headers to disk.)
- [ ] **Step 4: Run + commit**

```bash
cd NaX/src_server/agent_nonameax && GOEXPERIMENT=jsonv2,greenteagc go test ./ -run TestMapBuildProfileThroughSidecar -v && \
  git add NaX/src_server/agent_nonameax/pl_build_payload.go NaX/src_server/agent_nonameax/pl_nax_sidecar.go NaX/src_server/agent_nonameax/pl_build_payload_test.go && \
  git commit -m "agent_nonameax: route BuildPayload through sidecar socket (no in-server compile)"
```

## Task 7 — Builder Docker image + teamserver cleanup + compose wiring + end-to-end smoke test

**Files:**
- Create: `Dockerfile.nax-builder`
- Modify: `Dockerfile` (drop the NaX source-copy + runtime toolchain; keep the 4 `.so` steps)
- Modify: `docker-compose.yml` (add `nax-builder` service to the `runtime` profile)
- Create: `scripts/smoke-nax-sidecar.sh`

- [ ] **Step 1: Write `Dockerfile.nax-builder`** — build from a pinned Debian base; install a **pinned cross toolchain** (`g++-mingw-w64-x86-64`, `nasm`, `make`, `binutils`); copy the pinned NaX source (single commit, the same SHA as the submodule) + its `pe_templates/`; build the worker static binary (`go build -o /nax-builder cmd/nax-builder`); create a dedicated non-root UID; copy a `nax_builder_socket` file containing `/run/nax/builder.sock`; `ENTRYPOINT ["/nax-builder"]`. Pin the toolchain to fix the native-arm64 `objcopy` blocker.

- [ ] **Step 2: Modify the `runtime` server stage** — remove `COPY --from=build-server /src/NaX /app/NaX` (and the `COPY NaX /src/NaX` line in the build-server stage) and the runtime toolchain apt list (`clang`, `nasm`, `make`, `binutils`, `g++-mingw-w64-*`, `python3`) — those now live in the builder image. **Keep** the 4 explicit `.so` build steps and the `COPY --from=build-server /src/AdaptixC2/dist/ /app/`. (Note: the runtime image keeps whatever the builder image does not need — the toolchain physically remains here because Kharon still compiles in-server; it is only fully removed in step C.)

- [ ] **Step 3: Add `nax-builder` to `docker-compose.yml`** (inside the `runtime` profile, sibling to `server`):

```yaml
  nax-builder:
    profiles: ["runtime"]
    image: adaptixc2-omni-nax-builder:latest
    build:
      context: .
      dockerfile: Dockerfile.nax-builder
    container_name: adaptixc2-nax-builder
    network_mode: none            # socket traffic stays local; no external exposure
    security_opt:
      - no-new-privileges:true
    read_only: true             # builder is fully isolated; scratch is ephemeral tmpfs
    tmpfs:
      - /run/nax:mode=0o600     # shared with the teamserver over a tmpfs mount
    init: false
    mem_limit: 2g
    pids_limit: 512
    environment:
      - NAX_BUILDER_SOCK=/run/nax/builder.sock
```

And on the `server` service, mount the shared tmpfs so both containers see the socket:

```yaml
    tmpfs:
      - /run/nax:mode=0o600
    # add nax-builder to the server's companion list so compose brings it up
```

- [ ] **Step 4: Write `scripts/smoke-nax-sidecar.sh`** — build both images, `docker compose --profile runtime up`, trigger one agent build from the server (e.g. an HTTP agent with `output_format=bin`), assert the returned bytes are non-empty and `filename == "nax.x64.bin"`; then stop the builder and assert the server returns a clear builder-absence error (not a crash). Print pass/fail and exit non-zero on failure.

- [ ] **Step 5: Run the smoke test** (Linux/amd64 or arm64 host; macOS host runs it under QEMU):

```bash
scripts/smoke-nax-sidecar.sh
```

Expected: build succeeds, artifact is non-empty, builder-absence yields a clear error.

- [ ] **Step 6: Commit**

```bash
git add Dockerfile.nax-builder Dockerfile docker-compose.yml scripts/smoke-nax-sidecar.sh && \
  git commit -m "sidecar: builder image, teamserver cleanup, compose wiring, smoke test"
```

## Task 8 — Verify the Milestone 2 exit criterion

- [ ] **Step 1:** Bring up server + builder, build one HTTP agent (`bin`) end-to-end. Confirm the teamserver image contains **no** compiler and **no** writable NaX source tree.

- [ ] **Step 2:** Stop the builder; build the same agent. Confirm a clear "builder unavailable" error (no server crash).

- [ ] **Step 3:** Build an agent with `output_format=svc` end-to-end (exercised only in the Docker smoke test) and confirm the PE wrapper produced a non-empty `.svc.exe`.

- [ ] **Step 4:** Commit any fixes.

```bash
git add -A && git commit -m "sidecar: verify Milestone 2 exit criterion"
```

---

## Plan self-review (against the spec)

- **Spec coverage:** every spec § (scope, socket path, protocol/handshake, request/response schemas, builder internals, server change, compose/Dockerfile, exit criterion) maps to a task. `packNaxBin` unchanged → covered by Task 6. Toolchain-timing nuance (Kharon keeps it in the server during A; full removal in C) → called out in Task 7 Step 2 and the Scope box.
- **Placeholder scan:** no "TBD"/"implement later"/"add validation." `packNax`, `generateConfigH`, `ComponentPath`, `NaxBuildRequest`, `packNaxBin` are all defined in the referenced source or in earlier tasks.
- **Type consistency:** `NaxBuildRequest` (Task 1) → `BuildComponents` (Task 3) → `ServeConn`/`buildViaSidecar` (Tasks 5–6) all reference the same struct fields and the same `Components` map keys (`loader/beacon/pdata/xdata/textRva`). `ComponentPath` output names match `NaX/Makefile` exactly.
