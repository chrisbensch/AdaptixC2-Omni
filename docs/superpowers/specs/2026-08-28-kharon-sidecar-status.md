# Kharon Sidecar (Milestone 3) — Status

Working session: begin moving the **Kharon** agent's in-server payload compilation
onto a sidecar, following the Nax-sidecar pattern (Milestone 2). All design
decisions are now resolved (see below); next step is the full spec.

> **State:** design phase, not yet implemented. No code written for Milestone 3.
> The Nax sidecar (Milestone 2) is the reference implementation for how this should
> look.
>
> **Update (2026-08-28, session 2):** all seven open questions resolved — see
> "Decisions made so far" below. Next step is drafting the full spec.

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

## Implementation plan (pending design approval)

Per repo process: this goes through the brainstorming → spec → writing-plans flow.
All open questions are resolved; next steps:

1. Draft design doc (this file → full spec).
2. Self-review the spec (placeholders, contradictions, scope, ambiguity).
3. User reviews the spec.
4. Invoke `writing-plans` skill to create the implementation plan.
