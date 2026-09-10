# Kharon Sidecar (Milestone 3) — Status

Working session: begin moving the **Kharon** agent's in-server payload compilation
onto a sidecar, following the Nax-sidecar pattern (Milestone 2). All design
decisions are now resolved (see below); next step is the full spec.

> **State: COMPLETE and merged to `main`.**
>
> This file is a historical design record. The design it captured is now shipped.
> Do **not** treat "next step is the full spec" / "resume point: draft the spec"
> as live — the spec, plan, implementation, and CI all landed. See
> "Completion summary" below for the as-built state and remaining open items.
>
> **Update (2026-08-28, session 3):** reference + Kharon code research complete
> (see "Research findings"). Paused before drafting the full spec. Resume point:
> draft the spec from this file, using the research findings as ground truth.
>
> **Update (2026-09-07, resumed):** Milestone 3 was implemented from this design and
> merged. All decisions marked CONFIRMED here were built as specified; the as-built
> result and anything still open are captured in "Completion summary".

---

## Goal

Remove Kharon's runtime payload compilation from the teamserver so the hardened
server image no longer ships a cross-compilation toolchain (clang++/gcc/nasm/make)
or the Kharon `src_beacon` / `src_loader` source trees. The server performs no
native compilation; it sends a build request over a unix socket and receives back
a compiled payload. Mirrors the Milestone 2 rationale:

- **Attack surface** — no compiler inside the privilege-reduced server (UID 10001,
  `cap_drop: ALL`, `no-new-privileges`).
- **Network isolation** — the builder runs with `network_mode: none`; native PE
  compilation never touches the C2 network path.
- **Defense in depth** — two compromise surfaces (server vs builder); the builder
  is network-isolated, the server can't compile on its own.

This is the step where the toolchain *fully* leaves the server (Kharon was deferred
through Milestone 2 precisely because it still compiles in-server, which is what
kept `read_only` off).

**This has now happened.** Both payload builders run as off-server sidecars; the
runtime server image ships no cross toolchain, and `read_only` defaults to `true`.

---

## What Kharon compiles at runtime

`AgentGenerateBuild(agentConfig, agentProfile, listenerMap)` in
`Kharon/agent_kharon/src_server/pl_agent.go`:

1. Parses `agentConfig` (JSON), decodes the base64 malleable HTTP profile, builds
   ~25 `make` variables (sleep/jitter/uuid/worktime/killdate/forkpipe/spawnto/bof
   hook/malleable-bytes/callback-count/guardrails/syscall/bypass/heap-mask/sleepmask).
2. `make -C <src_beacon> x64|x86 [debug] <vars>` → compiles Config.cc + beacon →
   `Bin/Kharon.<target>.bin`.
3. If format is **Exe/Dll/Svc**: generates `Shellcode.h` from the beacon, writes it
   into `src_loader/Include/`, then compiles the loader source with
   `clang++ -target x86_64-w64-mingw32` → `Kharon.x64.exe/.dll/.svc.exe`.
4. If format is **Bin**: returns the raw beacon.

Key detail — paths are **cwd-dependent**:
`filepath.Join(filepath.Dir(wd), "dist", "extenders", "agent_kharon", "src_beacon")`
where `wd = os.Getwd()`. The sidecar must own its source tree and make these
deterministic (see open questions).

Further findings from `pl_agent.go` review:

- **x86 is a beacon-only path.** `Format == "x86"` only selects the make target
  (`Kharon.x86.bin`, raw beacon). The loader (Exe/Dll/Svc) is *always* compiled
  with `clang++ -target x86_64-w64-mingw32` — upstream has no x86 loader. So
  "x86 support" in the sidecar means only that its make step needs i686 mingw.
- **The server mutates the source tree.** For Exe/Dll/Svc it writes a generated
  `Shellcode.h` into `src_loader/Include/` before compiling. The sidecar must
  own this write (from beacon bytes in the request), not the server.

`ModuleDir` is set in `InitPlugin` (`Kharon/agent_kharon/src_server/pl_main.go:153`),
so the same `ModuleDir/nax_builder_socket` socket-resolution pattern applies.

---

## Decisions made so far

### 1. Scope — **CONFIRMED**: agent payload build only
Only the **agent payload build** moves: `src_beacon` make + optional `src_loader`
clang++ wrapper. **Stays in the server:**
- Kharon HTTP listener (does not compile at runtime).
- `src_core` precompiled BOF modules (`dist/*.x64.o`) — loaded at command exec,
  not compiled per-payload.

### 2. Approach: **B — separate Kharon sidecar**
Parallel to `sidecar/nax-builder/`, its own image + socket, fully isolated from
Nax. Chosen because Nax and Kharon builds share almost nothing (different
Makefiles, different header strategies, different output shapes), so one worker
would add coupling for little gain.

### 3. Build protocol — **CONFIRMED: A — reuse `naxbuilder` framing**
The server plugin dials the Kharon socket using the same length-prefixed JSON
framing as `naxbuilder` (dial + frame code reused), with Kharon-only message
types in a new `kharonbuilder` package. Alternatives B (fresh module) and C
(shared sidecarlib) rejected — C is premature refactoring for one consumer.

### 4. Socket + volume — **CONFIRMED**
Separate socket `/run/kharon/builder.sock` and its own shared named volume,
fully distinct from the Nax sidecar's `/run/nax`.

### 5. Source-tree ownership — **CONFIRMED**
The sidecar image bakes `src_beacon` + `src_loader` at a fixed path (e.g.
`/app/kharon/...`). The request carries all make vars + beacon bytes; for
loader builds the **sidecar** generates `Shellcode.h` from those bytes and owns
the write. The server plugin stops doing any local path resolution.

### 6. x86 — **CONFIRMED: support both targets**
The sidecar ships i686 mingw alongside x86_64 and handles both make targets
(`x64`, `x86`), preserving current operator-facing behavior. (Loader remains
x64-only — that is an upstream limitation, unchanged.)

### 7. Concurrency — **CONFIRMED: mutex serialization**
Builds are serialized with a mutex, matching the Nax sidecar. Simpler than
per-request temp workspaces; payload generation is not latency-critical.

### 8. Return contract — **CONFIRMED**
The sidecar returns compiled bytes + output filename (plus build logs for
operator visibility). The server plugin repacks exactly as today: raw beacon
for `Bin`/x86, loader exe/dll/svc otherwise.

---

## Research findings (session 3 — ground truth for the spec)

### Nax reference implementation (`sidecar/nax-builder/` + wiring)

- **Module layout:** `sidecar/nax-builder/{go.mod, cmd/nax-builder/main.go,
  naxbuilder/*.go}`. Package `naxbuilder` contains: `frame.go` (length-prefixed
  JSON framing, `MaxFrameBytes` = 64 MiB, `WriteFrame`/`ReadFrame`/`ServeConn`
  — one request/response per connection), `request.go` (`NaxBuildRequest` with
  allowlist `Validate()`, `NaxBuildResponse{Filename, Size, SHA256,
  Components map[string][]byte, OK}`, `NaxBuildError`), `worker.go`
  (`StartListener` on the unix socket with 0o660 chmod, `buildMu sync.Mutex`
  serialization, `ResolveSocketPath(moduleDir)` reading
  `<moduleDir>/nax_builder_socket` with `/run/nax/builder.sock` fallback,
  `Client.Build` = dial + frame), `build.go` (make invocation + component read).
- **Server-side patch pattern** (`patches/adaptix-nax-sidecar-task6.patch`,
  applied in the `build-server` stage with `patch -p2`): adds a local
  `replace ... => ../../../sidecar/nax-builder` to the agent's go.mod, adds a
  new `pl_nax_sidecar.go` (`buildViaSidecar`: dial → send request → repack),
  rewrites `BuildPayload` to build the fully-derived request and delegate, and
  adds a test with an in-process fake builder over a unix socket.
- **Builder image** (`Dockerfile.nax-builder`): golang build stage →
  debian:bookworm-slim runtime with the cross toolchain, dedicated non-root
  user `naxb` (UID/GID 10002), pinned source tree at `/nax`, patches applied,
  **invariant objects prebuilt at image build** (`make components ...`),
  `ENV NAX_BUILDER_SOCK=/run/nax/builder.sock`, static worker binary as
  ENTRYPOINT.
- **Compose wiring** (`docker-compose.yml`): `nax-builder` service in the
  `runtime` profile — `network_mode: none`, `user: naxb`, `cap_drop: ALL` +
  `DAC_OVERRIDE/CHOWN/SETUID/SETGID`, `no-new-privileges`, mem/pids limits,
  bounded JSON log, named volume `nax-sock:/run/nax` shared with the server.
  Server side: `usermod -a -G naxb adaptix` in the Dockerfile so it can
  connect() to the 0o660 socket; `nax-sock:/run/nax` mount on the server.

### Kharon build path (what moves)

- `AgentGenerateBuild` (`Kharon/agent_kharon/src_server/pl_agent.go:60`) parses
  `AgentConfig` JSON + base64 malleable profile, builds ~30 make vars
  (WEB_SECURE/PROXY_*, KH_SLEEP_TIME/JITTER/AGENT_UUID, worktime, killdate,
  forkpipe/spawnto, BOF hook, `HTTP_MALLEABLE_BYTES` hex array +
  `HTTP_CALLBACK_COUNT`, guardrails, KH_SYSCALL, KH_AMSI_ETW_BYPASS,
  KH_HEAP_MASK, KH_SLEEP_MASK), then `make -C <src_beacon> x64|x86[-debug]
  <vars>` → reads `Bin/Kharon.<target>.bin`.
- Exe/Dll/Svc: writes `Shellcode.h` (from `gen_shelllcode_header`, a ~30-line
  pure-Go function in `pl_utils.go:2070`) into `src_loader/Include/`, then
  `clang++ -target x86_64-w64-mingw32 ... Exe.cc|Dll.cc|Svc.cc` →
  `Kharon.x64.{exe,dll,svc.exe}`. **Loader is x64-only upstream.**
- `src_beacon/Makefile`: targets `x64`/`x86` (+ `-debug` variants); invariant
  objects prebuilt via `prebuild-x64`/`prebuild-x86` (Config.cc excluded,
  compiled per request); toolchain is `clang++ -target *-w64-mingw32` +
  `nasm`. Note: the plugin Makefile's `agent` target only runs
  `prebuild-x64` — the sidecar image must run **both** prebuilds.
- `ModuleDir` is set in `InitPlugin` (`pl_main.go:153`) — the
  `kharon_builder_socket` file pattern applies directly.
- `patches/kharon-core-mingw-compat.patch` guards Bookworm mingw-w64 12.2
  `PROCESS_MITIGATION_*` gaps in **src_core** headers only — not needed by the
  sidecar image (it never builds src_core).
- Sidecar toolchain set = what the server runtime stage currently carries for
  Kharon: `clang, nasm, make, binutils, g++-mingw-w64-x86-64,
  g++-mingw-w64-i686` (python3 is for other stages — verify before dropping
  from the server image).

### ⚠ Finding: in-server Kharon build appears broken in the runtime image

`pl_agent.go:93` resolves the source tree as
`filepath.Join(filepath.Dir(os.Getwd()), "dist", "extenders", "agent_kharon",
"src_beacon")` — i.e. it assumes the server runs with cwd = `<repo>/dist`
(the upstream dev flow: `cd dist && ./adaptixserver`). Our runtime image
flattens `dist/` into `/app` and runs with cwd `/app`, so this resolves to
`/dist/extenders/agent_kharon/src_beacon`, which does not exist (the tree is
at `/app/extenders/agent_kharon/src_beacon`). Payload generation in the
runtime image therefore appears to fail with "target path not found" — or was
never exercised end-to-end. **Verify during implementation** (a quick smoke:
start the runtime image, request a Kharon payload). The sidecar design
replaces this cwd-dependent logic with deterministic in-builder paths, which
fixes it as a side effect — worth calling out explicitly in the spec.

### Spec draft checklist (carried into the full spec)

1. `sidecar/kharon-builder/` module: reuse naxbuilder framing; new
   `kharonbuilder` package with Kharon request/response types (make vars as
   typed fields, beacon bytes for loader builds, format/target/debug).
2. Worker: owns `/app/kharon/{src_beacon,src_loader}`; generates `Shellcode.h`
   itself (port of `gen_shelllcode_header`); mutex-serialized; returns bytes +
   filename + build log.
3. `Dockerfile.kharon-builder`: mirror nax-builder shape; prebuild-x64 +
   prebuild-x86 at image build; dedicated user (e.g. `kharb`, UID 10003).
4. Server patch (`patches/adaptix-kharon-sidecar-*.patch`, `patch -p2` in
   build-server): go.mod replace, new `pl_kharon_sidecar.go`, rewrite
   `AgentGenerateBuild` to derive the request and delegate; in-process fake-
   builder test.
5. Runtime image: drop `clang/nasm/make/binutils/g++-mingw-*` (verify python3
   first) and the `src_beacon`/`src_loader` COPYs (keep `src_core`); add
   `usermod -a -G kharb adaptix`; then flip compose default to
   `read_only: true`.
6. Compose: `kharon-builder` service + `kharon-sock` named volume, mirroring
   the nax-builder block.
7. CI: extend `.github/workflows/build.yml` smoke test to build +
   smoke-test the kharon-builder image (amd64 + arm64), like nax-builder.

---

## Completion summary (as-built, merged)

Milestone 3 is **implemented and merged to `main`** (commits `a29b218` "Kharon
sidecar (#11)" and `18c5bc9` "CI-parity build + smoke-test the nax-builder
sidecar (amd64 + arm64)"). Every decision marked CONFIRMED in this design was
built as specified.

**As-built state:**

- Two sidecars under `sidecar/`: `kharon-builder/` (`kharonbuilder` pkg) and
  `nax-builder/` (`naxbuilder` pkg). Each is a separate image + unix-socket
  worker; the server performs no native compilation.
- Kharon: the Go plugin (`agent_kharon.so`) still builds in the server image, but
  the beacon + loader compile off-server in `kharon-builder` (network-isolated,
  `network_mode: none`). Nax mirrors this.
- `docker-compose.yml`: `kharon-builder` + `nax-builder` services, each with its
  own named socket volume (`kharon-sock:/run/kharon`, `nax-sock:/run/nax`). The
  server is joined to the `kharonb`/`naxb` groups to dial the 0o660 sockets. The
  `builder` service defaults to `read_only: true` (`ADAPTIX_READ_ONLY:-true`).
- CI (`.github/workflows/build.yml`, amd64 + arm64): builds **both** builder
  images and smoke-tests each end-to-end over its unix socket (copy the pinned
  source tree out of the image, start the builder, run the in-repo smoke client
  sharing the socket + source volume).
- Server image: cross toolchain and `src_beacon`/`src_loader` removed; `src_core`
  kept.

**Still open (non-blocking):**

- **NaX real-profile decode** — the nax smoke synthesizes a value-trivial
  (`0x00`) `Config_profile.h`, so the beacon *compiles* but does not decode into a
  live C2 callback. A real-profile run needs the server's pure-Go generators + a
  running teamserver (BLUEPRINT §10).
- **NaX header-writing ownership** — the request carries `ConfigH`/
  `ConfigProfileH`/`ConfigSleepmaskH` (base64); unresolved question is whether the
  worker should own writing these generated headers into `src_beacon/include/`
  (current approach) or that should happen elsewhere.
- **gin HTTP-routing 404** (upstream AdaptixC2) remains a known, accepted
  out-of-scope framework limitation; the sidecars build over the socket and are
  unaffected.

**Historical note (do not act on):** earlier revisions of this file and the
project memory described Milestone 3 as "design phase / partially built /
uncommitted / pending wiring and CI." That is stale — the work is merged. The
implementation lessons in "Research findings" (patch path conventions, `go work
sync` file deletion, mingw `-I` paths, objcopy fixes, socket-on-named-volume) are
still the durable reference.
