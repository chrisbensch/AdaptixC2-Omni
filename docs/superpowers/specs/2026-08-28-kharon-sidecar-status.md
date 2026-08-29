# Kharon Sidecar (Milestone 3) — Status

Working session: begin moving the **Kharon** agent's in-server payload compilation
onto a sidecar, following the Nax-sidecar pattern (Milestone 2). Session paused
after scope + approach were established; open questions captured below for the
next session.

> **State:** design phase, not yet implemented. No code written for Milestone 3.
> The Nax sidecar (Milestone 2) is the reference implementation for how this should
> look.

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

`ModuleDir` is set in `InitPlugin` (`Kharon/agent_kharon/src_server/pl_main.go:153`),
so the same `ModuleDir/nax_builder_socket` socket-resolution pattern applies.

---

## Decisions made so far

### 1. Scope (assumed — needs confirmation)
Only the **agent payload build** moves: `src_beacon` make + optional `src_loader`
clang++ wrapper. **Stays in the server:**
- Kharon HTTP listener (does not compile at runtime).
- `src_core` precompiled BOF modules (`dist/*.x64.o`) — loaded at command exec,
  not compiled per-payload.

### 2. Approach: **B — separate Kharon sidecar**
Parallel to `nax-builder`, its own image + socket, fully isolated from Nax. Chosen
because Nax and Kharon builds share almost nothing (different Makefiles, different
header strategies, different output shapes), so one worker would add coupling for
little gain.

### 3. Build protocol: leaning **A — reuse `naxbuilder` client + framing**
The server plugin would call the existing generic `naxbuilder.Client` (dial +
length-prefixed JSON framing) and define Kharon-only message types in a small
`kharonbuilder` package. Not yet confirmed — alternatives B (fresh module) and C
(shared sidecarlib) were discussed.

---

## Open questions (next session, in order)

1. **Confirm scope** — agent payload build only? Any reason to touch listeners or
   `src_core`?
2. **Build protocol** — A (reuse `naxbuilder` framing + Kharon message types) vs B
   (fresh module) vs C (shared lib). Recommendation: A.
3. **Socket + volume layout** — separate socket path (`/run/kharon/builder.sock`)
   and its own shared volume/tmpfs, distinct from the Nax sidecar's `/run/nax`.
4. **Source-tree ownership** — Kharon's build uses cwd-dependent paths; the sidecar
   must own its tree and make `src_beacon` / `src_loader` locations deterministic.
5. **x86 support** — Kharon builds both x64 and x86 (`cfg.Format == "x86"` → target
   `x86`). Decide whether the sidecar handles both (affects toolchain: x86 mingw)
   or just x64.
6. **Concurrency** — Kharon writes into its source tree (not thread-safe). Serialize
   builds with a mutex, or use per-request temp workspaces? (Nax sidecar serializes
   with a mutex.)
7. **Return contract** — what the sidecar returns (compiled bytes + filename) and
   how the server repacks/returns it to Beacon.

---

## Implementation plan (pending design approval)

Per repo process: this goes through the brainstorming → spec → writing-plans flow.
Next steps once open questions are resolved:

1. Draft design doc (this file → full spec).
2. Self-review the spec (placeholders, contradictions, scope, ambiguity).
3. User reviews the spec.
4. Invoke `writing-plans` skill to create the implementation plan.
