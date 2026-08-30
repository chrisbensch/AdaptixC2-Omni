# Kharon Sidecar (Milestone 3) — Full Spec

Working session: move the **Kharon** agent's in-server payload compilation onto a
sidecar, reusing the Milestone-2 Nax pattern. All design decisions are confirmed;
this is the complete design document. Reference implementation:
`sidecar/nax-builder/` (package `naxbuilder`).

> **State:** design complete; the sidecar implementation already exists but is
> **uncommitted and unwired**. This file is the source of truth for the
> implementation plan, which is primarily *wiring-in* + commit + docs, not a
> from-scratch build.
>
> **Already implemented (uncommitted, in the working tree):**
> - `sidecar/kharon-builder/` — full module (`kharonbuilder`: frame/request/worker/build/pe + tests).
> - `Dockerfile.kharon-builder` — standalone builder image (with the arm64 objcopy fix baked in).
> - `patches/adaptix-kharon-sidecar.patch` — server integration (4 hunks).
> - `patches/kharon-beacon-objcopy.patch` — arm64 objcopy fix.
> - `tmp_pl_kharon_sidecar.go` / `tmp_pl_kharon_sidecar_test.go` — root scratch prototypes (to be removed).
>
> **Not yet done:** main-Dockerfile runtime slimming, `docker-compose.yml` service + volume, CI smoke test, README/BLUEPRINT updates.
>
> **Parent status doc:** `2026-08-28-kharon-sidecar-status.md` (open questions +
> confirmed decisions + session-3 research findings). This file expands those into
> a complete design; it is the source of truth for the implementation plan.

---

## 1. Goal & rationale

Remove Kharon's runtime payload compilation from the teamserver so the hardened
server image no longer ships a cross-compilation toolchain (`clang++`/`gcc`/`nasm`/
`make`). The server performs no native compilation — it sends a build request over a
unix socket and receives a compiled payload back. Mirrors the Milestone-2 rationale:

- **Attack surface** — no compiler inside the privilege-reduced server
  (UID 10001, `cap_drop: ALL`, `no-new-privileges`).
- **Network isolation** — the builder runs with `network_mode: none`; native PE
  compilation never touches the C2 network path.
- **Defense in depth** — two compromise surfaces (server vs builder); the builder
  is network-isolated, and the server cannot compile on its own.
- **Smaller attack surface on the hardened image** — dropping the compiler removes a
  large class of toolchain CVEs and of code an exploited server plugin could shell
  out to.

**Milestone-3-specific rationale (why this exists at all):** Milestone 2 moved the
Nax agent's compilation off the server, but the Kharon agent *still compiles in
server* (`clang++`/`nasm`/`make`/`objcopy` in the runtime image). So the server
cannot yet run `read_only: true`. This milestone delivers that: once Kharon's
compilation is offloaded, the server image can drop the toolchain entirely and flip
to `read_only: true` by default — the hardening goal that Milestone 3 was opened to
achieve.

---

## 2. Scope

**In scope — the agent payload build only:**

- `src_beacon` make invocation (`make x64` / `make x86`, ±`-debug`) that produces
  the raw PIC beacon `.bin` (via `objcopy` of the temp `.exe`).
- The optional `src_loader` `clang++` step that wraps that beacon in a PE
  (`Exe.cc` / `Dll.cc` / `Svc.cc`) for the `Exe` / `Dll` / `Svc` formats.

**Out of scope — deliberately left in-server (unchanged):**

- The **Kharon HTTP listener** (`listener_kharon_http`) — no runtime compilation.
- The **precompiled Kharon core BOFs** (`src_core/dist/*.x64.o`) — loaded at command
  execution time, invariant across deployments, not built per-payload.
- The Nax agent (Milestone 2) — untouched; Kharon is a separate, independent sidecar.

Rationale: the listener and the precompiled BOFs compile nothing at runtime. Only
the agent's per-payload beacon (+ optional loader) build moves.

---

## 3. Current in-server build path (ground truth — `pl_agent.go`)

`AgentGenerateBuild(agentConfig, agentProfile, listenerMap)` does, in order:

1. Parse `AgentConfig` JSON; base64-decode `listenerMap["uploaded_file"]` into the
   malleable HTTP profile string.
2. **Server keeps** `BuildMalleableHTTPBytes(malleable_str)` (pure Go, `pl_malleable.go`)
   → produces the malleable bytes + callback count.
3. `os.Getwd()` + `filepath.Dir(wd)` to resolve `src_beacon` and `src_loader` paths
   (cwd-dependent — see §16).
4. Parse listener config (SSL, proxy, proxy creds, killdate, working-time, sleep,
   forkpipe, spawnto, BOF-hook, guardrails, syscall method, AMSI/ETW bypass, heap
   mask, sleep-mask) → build ~30 `make` variables.
5. **Make:** `make -C <src_beacon> <target> <vars…>` where `<target>` is
   `x64` / `x86` / `x64-debug` / `x86-debug`. Reads
   `<src_beacon>/Bin/Kharon.<target>.bin`.
6. For `Exe`/`Dll`/`Svc`: **server keeps** `gen_shelllcode_header` (pure Go,
   `pl_utils.go:2070`, ~30 lines) → writes `src_loader/Include/Shellcode.h`; then
   **clang++** the loader source (`Exe.cc`/`Dll.cc`/`Svc.cc`) →
   `src_loader/Bin/Kharon.x64.{exe,dll,svc.exe}`.
7. Return `(finalBin, outFileName, nil)`.

**What moves to the sidecar:** steps 5 (make) + 6 (Shellcode.h write + clang++).
**What stays in-server:** step 2 (malleable bytes), step 4 (parsing/escaping), step 7
(return contract). The server keeps the pure-Go `BuildMalleableHTTPBytes` and passes
its outputs (malleable hex + callback count) into the build request. Under the
preferred Option A (§8), `gen_shelllcode_header` does **not** stay in-server — the
sidecar ports it as `kharonbuilder.generateKharonShellcodeH` (`kharonbuilder/pe.go`)
and generates `Shellcode.h` itself from the beacon it builds. (`bool_to_int` and
`bytes_to_hexstr` are helpers folded into the server's request construction.)

---

## 4. Confirmed design decisions

All eight were resolved in brainstorming (see the parent status doc). Restated here
with the ground-truth details that pin them:

1. **Scope** — agent payload build only (`src_beacon` make + optional `src_loader`
   clang++). Listener + precompiled `src_core` BOFs stay in-server.
2. **Separate sidecar** — `sidecar/kharon-builder/`, parallel to `sidecar/nax-builder/`,
   fully isolated from Nax (its own image, socket, volume, user).
3. **Protocol** — reuse `naxbuilder` framing (4-byte big-endian length prefix + JSON
   body, 64 MiB max). Kharon-specific request/response types in a new
   `kharonbuilder` package. One request/response per connection.
4. **Socket/volume** — `/run/kharon/builder.sock`, own shared named volume
   `kharon-sock`, distinct from Nax's `/run/nax`.
5. **Source-tree ownership** — sidecar bakes `src_beacon` + `src_loader` at a fixed
   path (`/app/kharon/…`). Request carries the make vars + beacon bytes; the sidecar
   generates `Shellcode.h` from those bytes and owns the write. The server stops all
   local path resolution.
6. **x86** — support both `x64` and `x86` make targets (i686 mingw included). Loader
   remains x64-only (upstream limitation, unchanged).
7. **Concurrency** — mutex serialization (matches Nax). Payload generation is not
   latency-critical.
8. **Return contract** — compiled bytes + output filename (+ build logs for
   operator visibility). Server repacks exactly as today.

---

## 5. Sidecar module layout

```
sidecar/kharon-builder/
├── go.mod                      # module .../sidecar/kharon-builder
├── Dockerfile.kharon-builder   # builder image (mirrors Dockerfile.nax-builder)
├── cmd/
│   └── kharon-builder/
│       └── main.go             # reads KHARON_SRC + KHARON_BUILDER_SOCK, StartListener
└── kharonbuilder/
    ├── frame.go                # reuse naxbuilder framing verbatim (copy)
    ├── request.go              # KharonBuildRequest / KharonBuildResponse / KharonBuildError
    ├── worker.go               # StartListener, handleRequest, ResolveSocketPath, Client
    ├── build.go                # make invocation + Shellcode.h + loader compile + readback
    ├── build_test.go           # fake-tree tests (make stubbed)
    ├── request_test.go         # Validate() allowlist tests
    └── smoke/
        └── main.go             # CI smoke client (pure Go; dials the socket)
```

`frame.go` is copied from `sidecar/nax-builder/naxbuilder/frame.go` unchanged — the
framing is transport-agnostic. `kharonbuilder` is a parallel package to `naxbuilder`;
the two never import each other.

---

## 6. Protocol — `kharonbuilder` package

### 6.1 `KharonBuildRequest`

The fully server-derived instruction for one payload build. Contains **no**
caller-provided paths, Make targets, or shell fragments — the worker chooses the
Make target and paths from `Target`/`Debug`/`OutputFormat`, mirroring the Nax
request. All make variables are typed fields (the server builds them, exactly as
`pl_agent.go` does):

```go
type KharonBuildRequest struct {
    // Build selection (worker maps these onto the Makefile).
    Target       string `json:"target"`       // "x64" | "x86" — allowlisted
    Debug        bool   `json:"debug"`
    OutputFormat string `json:"outputFormat"` // "bin"|"exe"|"dll"|"svc" — allowlisted

    // Make variables — mirror pl_agent.go makeVars exactly.
    WebSecureEnabled  bool   `json:"webSecureEnabled"`
    WebProxyEnabled   bool   `json:"webProxyEnabled"`
    WebProxyURL       string `json:"webProxyUrl"`
    WebProxyUsername  string `json:"webProxyUsername"`
    WebProxyPassword  string `json:"webProxyPassword"`
    KhSleepTime       string `json:"khSleepTime"`
    KhJitter          int    `json:"khJitter"`
    KhAgentUUID       string `json:"khAgentUuid"`
    KhWorktimeEnabled bool   `json:"khWorktimeEnabled"`
    KhWorktimeStartH  int    `json:"khWorktimeStartHour"`
    KhWorktimeStartM  int    `json:"khWorktimeStartMin"`
    KhWorktimeEndH    int    `json:"khWorktimeEndHour"`
    KhWorktimeEndM    int    `json:"khWorktimeEndMin"`
    KhKilldateEnabled bool   `json:"khKilldateEnabled"`
    KhKilldateDay     int    `json:"khKilldateDay"`
    KhKilldateMonth   int    `json:"khKilldateMonth"`
    KhKilldateYear    int    `json:"khKilldateYear"`
    KhForkPipeName    string `json:"khForkPipeName"`
    KhSpawnto         string `json:"khSpawnto"`
    KhBofHookEnabled  bool   `json:"khBofHookEnabled"`
    HTTPMalleableHex  string `json:"httpMalleableHex"` // C-array hex (bytes_to_hexstr output)
    HTTPCallbackCount int    `json:"httpCallbackCount"`
    KhGuardrailUser   string `json:"khGuardrailUser"`
    KhGuardrailDomain string `json:"khGuardrailDomain"`
    KhGuardrailHost   string `json:"khGuardrailHost"`
    KhGuardrailIP     string `json:"khGuardrailIp"`
    KhSyscall         int    `json:"khSyscall"`         // 0 | 1 | 2
    KhAmsiEtwBypass   int    `json:"khAmsiEtwBypass"`   // 0x0 | 0x1 | 0x2 | 0x3
    KhHeapMask        bool   `json:"khHeapMask"`
    KhSleepMask       int    `json:"khSleepMask"`       // 0 | 1 | 2 | 3

    // Raw beacon bytes for the selected target, produced by the server's
    // BuildMalleableHTTPBytes + the beacon build. Only used by the loader path
    // (the sidecar writes Shellcode.h from these). Nil for OutputFormat == "bin".
    BeaconBytes []byte `json:"beaconBytes,omitempty"`
}
```

`BeaconBytes` carries the raw PIC beacon the server already built **only if** the
server keeps building the beacon itself (see §10 option B). If instead the sidecar
produces the beacon via `make` (option A, the preferred path), `BeaconBytes` is
empty and the sidecar reads `<kharon>/src_beacon/Bin/Kharon.<target>.bin` after
`make`.

### 6.2 `KharonBuildResponse`

```go
type KharonBuildResponse struct {
    Filename string `json:"filename"`     // e.g. "Kharon.x64.exe"
    Size     int    `json:"size"`
    SHA256   string `json:"sha256"`
    Payload  []byte `json:"payload"`      // the compiled binary (raw beacon, or loader PE)
    Logs     string `json:"logs,omitempty"` // build logs for operator visibility
    OK       bool   `json:"ok"`
}

type KharonBuildError struct {
    Error string `json:"error"`
}
```

`[]byte` marshals to base64 over the wire; the server base64-decodes. `Logs` carries
the make/clang++ stdout+stderr trimmed to a bounded size (say 64 KiB) so a huge
build log can't blow up the response frame.

### 6.3 `KharonBuildError`

The body used on an error frame. No `OK` field, so the server distinguishes
success/failure by inspecting the JSON for `"ok": true` — same pattern as Nax.

### 6.4 `Validate()`

Enforces the small allowlists the worker relies on: `Target ∈ {x64, x86}`,
`OutputFormat ∈ {bin, exe, dll, svc}`, and (if `BeaconBytes` present) a sane upper
bound on its length. Anything else is malformed — rejected before any work happens.
`Debug` is a bool (no allowlist needed).

---

## 7. Worker — `kharonbuilder/worker.go`

Mirrors `naxbuilder/worker.go`:

- **`StartListener(sockPath)`** — `net.Listen("unix", …)`, widen the socket to
  `0o660` for the shared-group case, loop `Accept()` → per-connection goroutine →
  `ServeConn(conn, handleRequest)`.
- **`buildMu sync.Mutex`** — serializes builds (a build spawns `make` + `clang++`,
  process-heavy). Removed once concurrent serving is needed.
- **`handleRequest(req)`** — validate; build the beacon via `make`; for non-`bin`
  formats generate `Shellcode.h` + compile the loader; read back the final bytes;
  return a `KharonBuildResponse`.
- **`ResolveSocketPath(moduleDir)`** — read `<moduleDir>/kharon_builder_socket`
  (trimmed, absolutized) if present; else `/run/kharon/builder.sock`. Same convention
  as Nax's `nax_builder_socket`.
- **`Client`** — `New(sock)`, `Build(req)` = dial → `WriteFrame` → `ReadFrame` →
  decode `KharonBuildResponse` (or return the `KharonBuildError` message as a Go
  error).

---

## 8. Build flow — `kharonbuilder/build.go`

The worker owns the entire build. Two sub-options for the beacon bytes:

**Option A (preferred — sidecar produces the beacon via `make`):**

1. `make -C <kharon>/src_beacon <target> <vars…>` where `<target>` is
   `x64`/`x86`/`x64-debug`/`x86-debug` (derived from `Target`+`Debug`).
2. Read `<kharon>/src_beacon/Bin/Kharon.<target>.bin`.
3. If `OutputFormat == "bin"` → return the raw beacon as `Payload`.
4. Else (`exe`/`dll`/`svc`) → generate `Shellcode.h` from the beacon, write it to
   `<kharon>/src_loader/Include/Shellcode.h`, then `clang++ …` the loader source →
   read back `<kharon>/src_loader/Bin/Kharon.x64.{exe,dll,svc.exe}`.

**Option B (server produces the beacon, sidecar only wraps):** the server keeps the
`make` for the beacon and passes `BeaconBytes` in the request; the sidecar skips
step 1–2 and writes `Shellcode.h` from `BeaconBytes` then compiles the loader.

**Decision: Option A.** Rationale: the beacon `.bin` is a build artifact that belongs
with the build; passing it over the socket duplicates a potentially large payload
across the wire for no reason, and it keeps the sidecar self-contained (one request,
one round-trip). The server should not be building the beacon at all once the
toolchain leaves the image — that is the whole point. `RunMake` is a package
variable so tests can stub it (mirrors Nax's `runMake`).

### 8.1 `Shellcode.h` generation

Port `pl_utils.go`'s `gen_shelllcode_header` verbatim into `kharonbuilder`
(`genShellcodeHeader([]byte) string`). It emits:

```c
#pragma once

// Autogenerated shellcode
#include <cstdint>

namespace Shellcode {

    constexpr size_t Size = <len>;

__attribute__((section(".text")))
const uint8_t Data[] = {
        0x.., 0x.., ...
    };

}
```

Written to `<kharon>/src_loader/Include/Shellcode.h`. The loader sources include it.

### 8.2 Loader compile

Mirror `pl_agent.go`'s clang++ invocation exactly:

```
clang++ -target x86_64-w64-mingw32 -I <kharon>/src_loader/Include \
    -o <kharon>/src_loader/Bin/<out> <kharon>/src_loader/Source/Main/<src> \
    -Os -mwindows -nostdlib -s -lkernel32 -ladvapi32 [-shared for Dll]
```

where `<src>`/`<out>` map from `OutputFormat`:

| OutputFormat | Source file  | Output name         |
|--------------|--------------|---------------------|
| `exe`        | `Exe.cc`     | `Kharon.x64.exe`    |
| `dll`        | `Dll.cc`     | `Kharon.x64.dll`    |
| `svc`        | `Svc.cc`     | `Kharon.x64.svc.exe`|

Note: the loader is **always** compiled as x64 (`-target x86_64-w64-mingw32`),
regardless of the beacon `Target`. This is an upstream limitation — preserve it. For
an x86 beacon + loader format the pairing is upstream's (arguably broken) behaviour;
the sidecar reproduces it faithfully.

### 8.3 Output filename

Mirror `pl_agent.go`'s final switch on `OutputFormat`:

- `exe` → `Kharon.x64.exe`
- `dll` → `Kharon.x64.dll`
- `svc` → `Kharon.x64.svc.exe`
- `bin` → `Kharon.x64.bin` (raw beacon)
- default → `Kharon.<target>.bin`

Compute `Payload` bytes, `Size`, and `SHA256`; attach the trimmed build `Logs`.

---

## 9. Server-side integration

### 9.1 New file — `pl_kharon_sidecar.go`

Adds `buildViaSidecar(req *kharonbuilder.KharonBuildRequest) ([]byte, string, error)`
and rewrites `AgentGenerateBuild` to derive the request inline and delegate. Mirrors
the Nax `pl_nax_sidecar.go` / `buildViaSidecar` pattern exactly:

1. Keep `BuildMalleableHTTPBytes` (server) → malleable bytes + callback count.
2. Build the ~30 make-var fields on `KharonBuildRequest` exactly as `pl_agent.go`
   does today (same parsing/escaping of SSL/proxy/killdate/worktime/sleep/forkpipe/
   spawnto/guardrails/syscall/bypass/heap/sleepmask).
3. Set `Target`/`Debug`/`OutputFormat` from `cfg`.
4. `buildViaSidecar(req)`:
   - Resolve the socket path from `ModuleDir/kharon_builder_socket` (fallback
     `/run/kharon/builder.sock`).
   - Dial → `kharonbuilder.WriteFrame` → `kharonbuilder.ReadFrame`.
   - On `OK:false` / error frame → return the builder's error message.
   - On success → return `(resp.Payload, resp.Filename, nil)`.
5. Never invokes a compiler or reads/writes a source tree in-server.

### 9.2 Patch file — `patches/adaptix-kharon-sidecar.patch`

Applied in the `build-server` stage with `patch -p2`, from `extenders/agent_kharon/`
(e.g. `RUN cd /src/AdaptixC2/AdaptixServer/extenders/agent_kharon && patch -p2 < <patch>`),
exactly as `patches/kharon-core-mingw-compat.patch` is applied. **Four hunks:**

1. **`src_server/pl_agent.go`** — add import + rewrite `AgentGenerateBuild` to derive
   the request and delegate to `buildViaSidecar` (drop the in-server `make` +
   `clang++` + cwd-dependent path resolution).
2. **`go.mod`** — add a local `replace` from the agent module to
   `../../../sidecar/kharon-builder`, so the agent module can import `kharonbuilder`
   without shipping a published reference.
3. **`src_server/pl_kharon_sidecar.go`** (**new file**) — `buildViaSidecar(req)`:
   dial → send request → repack; never invokes a compiler or touches a source tree.
4. **`src_server/pl_kharon_sidecar_test.go`** (**new file**) — in-process fake builder
   over a unix socket; assert the derived request fields and the repacked return value.

**Patch path convention (corrected).** Headers are relative to the Kharon submodule
root and **include the `agent_kharon/` component**, applied from
`extenders/agent_kharon/` with `-p2` (matching `kharon-core-mingw-compat.patch`):

```
a/agent_kharon/src_server/pl_agent.go
a/agent_kharon/go.mod
a/agent_kharon/src_server/pl_kharon_sidecar.go
a/agent_kharon/src_server/pl_kharon_sidecar_test.go
```

`-p2` drops `a/` + `agent_kharon/` so the files land in
`extenders/agent_kharon/src_server/…` (and `go.mod` in `extenders/agent_kharon/`).
Applying the old header `a/src_server/pl_agent.go` with `-p2` from
`extenders/agent_kharon/` is **wrong** — it would leave a stray
`extenders/agent_kharon/pl_agent.go` and never modify the real file. Applied before
`go work sync` so the patched go.mod's sidecar `replace` resolves into the workspace.

### 9.3 `kharon_builder_socket` file

The server's agent reads `<moduleDir>/kharon_builder_socket` to find the builder's
unix socket on the shared `/run/kharon` volume. Kept as a file so an operator can
override the path by editing it in `/app/extenders/agent_kharon/` without a rebuild.
The builder image writes `/run/kharon/builder.sock` into it.

---

## 10. `Dockerfile.kharon-builder`

Mirrors `Dockerfile.nax-builder` shape:

- **build stage** — `golang:1.25.14` (pinned digest, same as Nax) → `go build
  -ldflags="-s -w" -o /kharon-builder ./cmd/kharon-builder`.
- **runtime stage** — `debian:bookworm-slim` (pinned digest) with the cross
  toolchain the Kharon beacon needs: `clang`, `nasm`, `make`, `binutils`,
  `g++-mingw-w64-x86-64`, `g++-mingw-w64-i686`, `python3`, `patch`.
  Dedicated non-root user **`kharonb` (UID/GID 10003)** — distinct from Nax's
  `naxb` (10002).
- **⚠ arm64 objcopy fix (required, baked at image build).** On arm64 hosts the
  default `objcopy` is an ARM triple that cannot read the x86-64 PE the beacon
  Makefile links, so `objcopy --dump-section .text=…` fails and the `.bin` comes
  out 0 bytes. `patches/kharon-beacon-objcopy.patch` forces both beacon objcopy
  calls to `x86_64-w64-mingw32-objcopy` (from `g++-mingw-w64-x86_64`). Applied to
  `src_beacon/Makefile` with `patch -p1` before `make prebuild`. Without it, x64
  beacon builds are silently empty on arm64.
- `COPY Kharon /app/kharon` — bakes `src_beacon` + `src_loader` at a fixed path
  (`/app/kharon/…`), so the worker's paths are deterministic (no cwd resolution).
- Apply the Kharon BOF mingw-w64 Bookworm compat patch to `src_core` if needed for
  the builder (it isn't for the beacon/loader build, but harmless).
- **Prebuild at image build** — run `make -C /app/kharon/src_beacon prebuild-x64
  prebuild-x86` so runtime builds only compile `Config.cc` + link (seconds not
  minutes). Same trick as the Nax prebuild.
- `COPY --from=build /kharon-builder /kharon-builder`; `chown -R kharonb` the tree;
  `chmod 0755 /kharon-builder`.
- `ENV KHARON_SRC=/app/kharon`, `ENV KHARON_BUILDER_SOCK=/run/kharon/builder.sock`.
- Write `/run/kharon/builder.sock` into the socket file.
- `ENTRYPOINT ["/kharon-builder"]`.

Toolchain note: the beacon Makefile shells `clang++ -target x86_64-w64-mingw32` and
`nasm`; the loader uses `clang++ -target x86_64-w64-mingw32`. Both need `clang` +
the mingw headers (`g++-mingw-w64-*` provides them) + `nasm` + `objcopy`
(`binutils`). The i686 target additionally needs `g++-mingw-w64-i686`.

---

## 11. Runtime image changes (`Dockerfile` runtime stage)

- **Drop the toolchain** — remove `clang`, `nasm`, `make`, `binutils`,
  `g++-mingw-w64-x86-64`, `g++-mingw-w64-i686`, `python3` from the runtime
  `apt-get install` (verify `python3` isn't needed elsewhere first — it's used by
  other stages, not the runtime server).
- **Drop the source trees** — remove the `COPY --from=build-server … src_beacon` and
  `… src_loader` into the runtime image (they no longer compile there). Keep the
  precompiled `src_core/dist/*.x64.o` (loaded at command time, not built at runtime).
- **Drop the Kharon `go.work` entries** — remove the two `use` lines for
  `extenders/agent_kharon` and `extenders/listener_kharon_http` from the workspace
  (or keep the listener if it still builds in-server; the agent is now imported
  via the local `replace` only for the `kharonbuilder` package). Prefer removing the
  agent `use` since its build logic no longer runs in-server.
- **Add `kharonb` group** — `groupadd -g 10003 kharonb`; `usermod -aG kharonb
  adaptix`; `MODIFY_GID=10003` for the builder socket volume (matches the Nax
  pattern for the nax-builder socket).

Net effect: the hardened server image no longer contains a Windows cross-compiler,
`nasm`, `make`, or the Kharon agent/loader source trees. This is what enables
`read_only: true` (nothing compiles in-server). The Kharon agent still works: it
sends a build request over `/run/kharon/builder.sock` and receives a compiled
payload.

---

## 12. Compose wiring (`docker-compose.yml`)

Add a `kharon-builder` service + `kharon-sock` named volume, parallel to the Nax
service:

```yaml
  kharon-builder:
    profiles: ["build", "runtime"]
    image: adaptixc2-omni-kharon-builder:latest
    network_mode: none            # native PE compilation never touches the C2 net
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    read_only: true               # writes go to the kharon-sock volume only
    tmpfs: ["/tmp"]               # make's scratch objects (if not prebuilt in-img)
    environment:
      - KHARON_SRC=/app/kharon
      - KHARON_BUILDER_SOCK=/run/kharon/builder.sock
    restart: unless-stopped

  server:
    # (add alongside the existing nax-sock mount)
    volumes:
      - kharon-sock:/run/kharon
```

- `kharon-sock` named volume, `MODIFY_GID: 10003`, same rationale as `nax-sock`.
- `kharon-builder` mounts only `/run/kharon` (the shared socket dir); it never sees
  the C2 data volume, so a builder compromise can't read agent state.
- `network_mode: none` — the builder has no network path.
- Update the runtime server comment about `read_only` (it's now achievable because
  BOTH agents compile off-server).

---

## 13. CI (`build.yml`)

Add a Kharon-builder smoke test mirroring the Nax one:

```yaml
  kharon-builder-smoke:
    needs: [build]
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, ubuntu-24.04-arm]
    steps:
      - uses: actions/checkout@v4
      - run: docker build --target build -t adaptixc2-omni-kharon-builder:ci \
             --build-arg GO_WIN7_SHA=<pinned> . --dockerfile Dockerfile.kharon-builder
      - run: docker run --rm -v kharon-smoke:/run/kharon adaptixc2-omni-kharon-builder:ci \
             kharonbuilder-smoke --socket /run/kharon/builder.sock --target x64 --format bin
```

- Smoke test builds one Kharon beacon (x64, `bin`) and asserts the response is
  `OK:true` with a non-empty, correctly-named payload; a second run with `format
  exe` asserts the loader PE. Pure-Go client (`kharonbuilder/smoke`), no network.
- Runs on both amd64 + arm64 to catch cross-arch build drift.
- Optional: a full-runtime smoke (agent config → sidecar build → repack →
  SHA256 matches a golden value) once a beacon artifact is available.

---

## 14. Documentation updates

- **`README.md`** — note the Kharon sidecar alongside the Nax one; update the
  runtime-server `read_only` note (now on by default once the toolchain is dropped).
- **`BLUEPRINT.md`** — add a Kharon-sidecar section parallel to the Nax one: the
  builder image layout, the socket/volume, the `kharonb` user, the go.mod `replace`,
  the `kharon_builder_socket` override, the runtime slimming, and the CI smoke test.
  Update the "build the whole thing" table and the toolchain notes.
- **`patches/` README (if any)** — list the new Kharon patch file.
- **`Kharon/` docs** — note that compilation is now offloaded; no upstream change
  (the graft is workspace-only).

---

## 15. Implementation plan (milestones)

**Milestone 3a — sidecar package + build logic.**
Write `kharonbuilder` (frame/request/worker/build + tests). Verify with in-process
fake-builder unit tests (no toolchain needed).

**Milestone 3b — builder image + Dockerfile.kharon-builder.**
Build the image; verify `make` + `clang++` run inside it (real beacon + loader
build) and the smoke client talks to it.

**Milestone 3c — server integration + patch.**
`pl_kharon_sidecar.go` + `patches/adaptix-kharon-sidecar-*.patch` + go.mod `replace`.
Verify the server derives the request and repacks the response.

**Milestone 3d — runtime slimming + compose + CI.**
Dockerfile runtime stage + `kharon-builder` compose service + `kharon-sock` volume +
CI smoke test. Verify `read_only: true` runtime + CI parity.

**Milestone 3e — docs.**
README + BLUEPRINT updates.

---

## 16. Risks & open items

- **x86 loader pairing** — for an x86 beacon + loader format, the loader is still
  x64 (upstream). Preserve; document. Operators should use x64 beacon + loader.
- **`src_beacon` make vars** — the exact 30-var list and escaping must match
  `pl_agent.go` byte-for-byte or the beacon won't compile/parse the profile. Verify
  against the Makefile's expected variable names (already captured in §4).
- **cwd-dependent path bug** — the current in-server code resolves paths via
  `os.Getwd()/dist/extenders/…`, which is broken in the runtime image (cwd `/app`,
  resolves to non-existent `/dist/…`). The sidecar fixes this as a side effect by
  owning the source tree at a fixed path. Worth a verification pass during 3c.
- **`go.mod` replace path** — the local `replace` must point at the sidecar module
  from the agent module (relative path `../../../sidecar/kharon-builder`). Verify the
  workspace `use`/`replace` interaction.
- **Beacon size over socket** — a raw beacon is a few KB; the loader PE is tens of
  KB. Well under the 64 MiB frame cap. No concern.
- **Build logs** — trim to 64 KiB before sending; a runaway build shouldn't flood
  the response frame.
- **Concurrency** — mutex-serialized (matches Nax). If beacon builds become a
  bottleneck, parallelize later; not needed for correctness.
- **Prebuild marker** — `prebuild-x64`/`prebuild-x86` in the image build; verify the
  runtime make still compiles `Config.cc` correctly (the Makefile excludes
  `Config.cc` from prebuild and compiles it per-request — same as today).

---

## 17. Verification checklist

- [ ] `kharonbuilder` unit tests pass (fake builder, no toolchain).
- [ ] Builder image builds; smoke client produces a valid x64 beacon + a loader PE.
- [ ] Server derives the correct request and repacks the response (unit + integration).
- [ ] Runtime image: no `clang++`/`nasm`/`make`/`g++-mingw-w64` in the image; no
      `src_beacon`/`src_loader` source trees; `read_only: true` runs healthy.
- [ ] CI: Kharon-builder smoke on amd64 + arm64; full-runtime smoke.
- [ ] Trivy: no CRITICAL/HIGH regressions from the new builder image.
- [ ] BLUEPRINT + README updated.
