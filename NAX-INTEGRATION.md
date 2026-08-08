# NaX Integration Research and Project Timeline

This document records the investigation into integrating [MaorSabag/NaX](https://github.com/MaorSabag/NaX) with AdaptixC2-Omni. It is a planning and decision record, not a claim that NaX is currently included or supported.

Research was performed against NaX commit `bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3`.

## Objective

Add NaX as an optional Adaptix agent ecosystem without weakening the current Omni posture:

- One pinned, coordinated build for the Adaptix server and Go plugins.
- Native Linux amd64 and arm64 server images.
- Minimal runtime image.
- Read-only teamserver root filesystem.
- Teamserver running as UID 10001 with its current capability set.
- No native compiler toolchain or writable source tree in the exposed teamserver.
- Existing Trivy, SBOM, credential, TLS, fingerprint, and resource-limit controls retained.
- Existing default image and `profile.kharon.yaml` behavior unchanged until an experimental integration is proven.

## Research completed

- [x] Identified NaX's Adaptix plugin modules and API versions.
- [x] Compared NaX's Go requirements with the pinned AdaptixC2 server.
- [x] Inventoried the normal native build dependencies.
- [x] Reviewed NaX's setup/deployment assumptions.
- [x] Reviewed NaX's runtime artifact-generation path.
- [x] Assessed Linux amd64 and arm64 host-build implications.
- [x] Compared NaX's integration model with the existing Kharon graft.
- [x] Reviewed licensing and preliminary provenance concerns.
- [x] Designed a proposed runtime separation between the teamserver and native builders.
- [ ] Compile or load NaX against the pinned Adaptix server.
- [ ] Exercise NaX on native amd64 or arm64.
- [ ] Exercise the existing Kharon artifact-generation callback in the hardened runtime.
- [ ] Implement or test a builder protocol.
- [ ] Modify any production runtime or profile.

## Current findings

### Plugin and Go compatibility

NaX consists of four server-side modules:

- `agent_nonameax`
- `listener_nonameax_http`
- `listener_nonameax_smb`
- `service_nax_store`

The modules declare Go `1.25.4` and `github.com/Adaptix-Framework/axc2 v1.2.0`. This aligns with the pinned Adaptix v1.2 API and Omni's Go 1.25.12 build toolchain.

Go plugin ABI compatibility still requires the Adaptix server and every NaX `.so` to be compiled with the same exact Go toolchain, dependency selection, flags, source revisions, and `GOEXPERIMENT=jsonv2,greenteagc`. Independently downloaded or separately compiled `.so` files are not acceptable. The four modules should join the server's `go.work` and build in the same Docker stage as the server.

### Build dependencies

The normal NaX path uses:

- Go
- Make
- MinGW-w64 GCC/G++
- NASM
- GNU binutils, particularly `objcopy`
- Python 3

The normal Make-based path does not appear to fetch dependencies over the network. Exact native-tool versions are not pinned upstream.

The Windows target is x64. Linux amd64 and arm64 refer to the architecture of the teamserver and its Go plugins, not to the generated Windows artifact.

### Build-layout mismatch

NaX's setup and Makefiles expect a different `Server/extenders` layout and do not consistently implement AdaptixC2's local `dist/` packaging contract. A direct copy followed by `make server-ext` is insufficient.

An Omni integration must explicitly:

1. Copy all four modules into the disposable `AdaptixServer/extenders/` tree.
2. Add them to `go.work` and run `go work sync`.
3. Build the plugins with the server's exact Go environment.
4. Stage each `.so`, `config.yaml`, `ax_config.axs`, `pe_templates`, and every other required ancillary asset into `/app/extenders/<name>/`.
5. Avoid preserving build-stage absolute paths in `nax_root.conf`.

### arm64 blocker

NaX main uses host `objcopy` in paths that process Windows x64 files. On native Linux arm64 this can select an AArch64-target tool that cannot process the required x86-64 PE sections.

[NaX PR #2](https://github.com/MaorSabag/NaX/pull/2) proposes preferring `x86_64-w64-mingw32-objcopy`, but remains open. Omni should carry a stricter patch that requires the cross-target tool, fails when it is absent, and validates all produced files rather than falling back to host `objcopy`.

### Runtime-compilation blocker

NaX's current `BuildPayload` callback:

- Locates a complete NaX source tree through `nax_root.conf`.
- Writes generated configuration headers.
- Removes and recreates source-tree build/cache directories.
- Invokes Make and the native toolchain.
- Reads and packages the resulting files.

This is incompatible with Omni's current runtime image, which intentionally contains no compiler toolchain, runs with a read-only root filesystem, and only permits persistent writes beneath `/app/data`.

The following shortcuts are explicitly rejected:

- Installing Make, MinGW, NASM, Python, and source code in the teamserver image.
- Disabling `read_only`.
- Making an extender source directory writable by the teamserver.
- Persisting mutable compiler source and caches under `/app/data`.
- Mounting the Docker socket into the teamserver.
- Loading uncoordinated prebuilt Go plugins.

### Kharon comparison and likely gap

The Kharon graft provides the correct high-level precedent for copying modules into a disposable Adaptix tree, updating `go.work`, building plugins, staging assets, and registering profile entries.

Static inspection found a likely gap in the current hardened runtime integration:

- `Kharon/agent_kharon/src_server/pl_agent.go` expects `dist/extenders/agent_kharon/src_beacon`, invokes Make during `BuildPayload`, and invokes Clang for some wrapper formats.
- `Kharon/setup_kharon.sh` copies `src_beacon`, `src_loader`, `src_core`, and `src_modules` into the native distribution.
- The unified Dockerfile does not mirror that full source-directory copy and the runtime image does not include the required compiler toolchain.

The plugin can therefore load and the server can pass its health check while artifact generation still fails. This is not yet confirmed by an end-to-end runtime test and must be verified before the existing Kharon path is used as the exact NaX template.

### Post-ex tooling

Extension-Kit's command registration can be extended to another agent after its BOF API compatibility is validated. Persistent registration changes should be represented as workspace patches or generated build-time overlays, not edits inside the submodule.

`PostEx-Arsenal/kh_modules.axs` is Kharon-oriented. Commands should be reviewed individually before being registered for NaX; standard BOF wrappers may be reusable, while Kharon-specific behavior must remain restricted to Kharon.

### Maturity and provenance

NaX's root license is MIT and requires preservation of its copyright and license notice. Additional review is needed because the project:

- Has a young public history.
- Has no tagged releases or upstream CI at the reviewed revision.
- Contains checked-in PE executables.
- Credits Stardust-derived work without a component-level provenance and license inventory.
- Does not pin exact native-tool versions.

No required binary should be redistributed until it is traced to reviewed source and an applicable license.

## Decisions to date

1. **Do not add NaX to the default image yet.** Treat it as an optional experimental candidate.
2. **Keep the default runtime unchanged.** `profile.kharon.yaml`, the existing runtime target, and the normal Compose profile remain the supported default during evaluation.
3. **Build Go plugins with the server.** Only the native Windows artifact builds are candidates for a separate builder.
4. **Do not compile inside the teamserver.** Runtime native compilation belongs behind a separate trust boundary.
5. **Use a shared protocol but separate agent builders.** Kharon and NaX should have distinct builder images, source trees, sockets, resource limits, SBOMs, and upgrade lifecycles.
6. **Prefer eliminating runtime compilation long-term.** Prebuild invariant code and represent request-specific settings as a bounded, versioned data block.
7. **Use a builder sidecar only as an interim design where compilation remains necessary.**
8. **Keep experimental state separate.** Use an experimental image/profile and a separate data directory rather than silently modifying an existing first-start-rendered profile.

## Proposed separation

```text
                              kharon-builder.sock
Adaptix teamserver/plugin  ─────────────────────────▶ Kharon builder
       read-only           \
       UID 10001            └────────────────────────▶ NaX builder
                              nax-builder.sock
```

### Teamserver and plugins

The teamserver retains:

- Adaptix request parsing.
- Agent configuration validation.
- Conversion to a bounded builder request.
- Unix-socket client behavior.
- Builder identity/version verification.
- Response hash, size, type, and filename validation.
- Returning bytes or a clear error through Adaptix's existing synchronous `BuildPayload` API.

The teamserver must not retain:

- Agent source trees.
- Generated C/C++ headers.
- Makefiles or compiler toolchains.
- Writable native build caches.
- Arbitrary subprocess execution.

### Per-agent builder

Each builder contains:

- One pinned upstream source revision.
- Reviewed Omni patches.
- Fixed native toolchains.
- An adapter for that upstream's build process.
- Root-owned read-only source in the image.
- A fresh bounded tmpfs workspace per request.

Each builder runs:

- As a dedicated non-root UID.
- With `network_mode: none`.
- With a read-only root filesystem.
- With all capabilities dropped and `no-new-privileges`.
- Under CPU, memory, PID, timeout, scratch-size, request-size, and output-size limits.
- Without host bind mounts, the Docker socket, teamserver state, or persistent source/build caches.

The source is copied into the per-request workspace, configuration is rendered there, fixed build commands execute with argument arrays, the result is validated and returned through the Unix socket, and the workspace is destroyed after success, failure, timeout, or cancellation.

### Protocol requirements

- Versioned request and response schemas.
- Allowlisted agent, transport, build mode, variant, and output format values.
- No caller-provided source paths, output paths, Make targets, compiler arguments, or shell fragments.
- Bounded strings, collections, request bytes, response bytes, and diagnostics.
- Request ID and deadline.
- Builder handshake containing upstream SHA, patch digest, protocol version, and toolchain identity.
- Artifact size, SHA-256, format, and variant metadata.
- Unix-socket ownership/permissions and peer verification.
- Sensitive settings excluded from logs.
- One request at a time per builder initially.

## Project timeline

Durations are provisional and assume upstream main builds successfully after the known arm64 patch.

### Milestone 0 — Validate the baseline (2–3 days)

- [ ] Build the current image.
- [ ] Exercise Kharon's `BuildPayload` inside the hardened runtime.
- [ ] Record missing paths/tools and actual failure behavior.
- [ ] Confirm whether all output formats share the same runtime compilation dependency.
- [ ] Add a regression-test design that goes beyond server health.

**Exit criterion:** the current Kharon runtime behavior is known rather than inferred.

### Milestone 1 — NaX compatibility spike (3–5 days)

- [ ] Add a temporary pinned NaX source checkout for evaluation.
- [ ] Apply the strict cross-`objcopy` patch.
- [ ] Graft four modules into the disposable Adaptix build tree.
- [ ] Build the server and plugins under one Go environment.
- [ ] Explicitly stage and inventory every required file.
- [ ] Load all four plugins on native amd64.
- [ ] Repeat on native arm64.
- [ ] Inventory checked-in binaries and third-party provenance.

**Exit criterion:** both native server architectures compile and load the four plugins, or concrete blockers are documented.

### Milestone 2 — Builder protocol prototype (4–7 days)

- [ ] Define a versioned request/response schema.
- [ ] Implement a minimal Unix-socket client in one agent plugin.
- [ ] Implement one non-networked builder worker.
- [ ] Build in a fresh tmpfs workspace.
- [ ] Return and verify artifact bytes and metadata.
- [ ] Handle builder absence, timeout, cancellation, malformed input, and malformed output.

**Exit criterion:** one deterministic end-to-end request succeeds without compilers or writable source in the teamserver.

### Milestone 3 — Separate Kharon and NaX builders (1–2 weeks)

- [ ] Create independent Kharon and NaX builder images and sockets.
- [ ] Patch both plugins to use the shared protocol.
- [ ] Add strict validation, cleanup, resource limits, and log redaction.
- [ ] Add experimental Docker and Compose profiles.
- [ ] Add `scripts/setup-agents.sh` or equivalent lifecycle wrapper.
- [ ] Generate separate SBOMs and provenance manifests.

**Exit criterion:** both agent build paths operate under the declared trust boundaries and resource controls.

### Milestone 4 — Post-ex and client integration (3–5 days)

- [ ] Add NaX to compatible Extension-Kit command registrations.
- [ ] Review PostEx-Arsenal wrappers individually for NaX compatibility.
- [ ] Validate AxScript registration and argument packing in the client.
- [ ] Exercise supported output types in an isolated Windows x64 lab.

**Exit criterion:** supported commands and output formats are explicitly documented and validated.

### Milestone 5 — Hardening and promotion decision (3–5 days)

- [ ] Run native amd64 and arm64 CI matrices.
- [ ] Assert the default image contains no NaX components or builder tooling.
- [ ] Assert experimental teamserver images contain no compiler toolchains or writable source.
- [ ] Test unavailable, stale, mismatched, concurrent, oversized, and resource-exhaustion cases.
- [ ] Run Trivy, SBOM, secret, and license checks for every image.
- [ ] Confirm rollback with existing persistent server state.
- [ ] Decide whether NaX remains experimental or can be promoted.

**Promotion criteria:** resolved provenance, stable plugin compatibility, dual-native-architecture CI, functional lab validation, no sensitive logging, and no reduction in teamserver hardening.

### Long-term milestone — Remove runtime compilation

- [ ] Identify invariant native objects and the minimum request-specific data.
- [ ] Define a versioned, bounds-checked binary configuration block.
- [ ] Build and hash an allowlisted variant matrix during image construction.
- [ ] Patch configuration in memory without executing subprocesses.
- [ ] Retire the corresponding builder when it is no longer required.

## Anticipated workspace changes

No files below have been changed for NaX yet. Likely future integration points are:

- `.gitmodules`
- `Dockerfile`
- `docker-compose.yml`
- `profile.nax.experimental.yaml`
- `patches/nax-*.patch`
- `patches/kharon-*.patch`
- `scripts/setup-agents.sh`
- `.github/workflows/build.yml`
- `README.md`
- `BLUEPRINT.md`
- `CLAUDE.md`

## Verification checklist

- [ ] Default image remains byte-functionally unchanged and excludes NaX.
- [ ] Server and all Go plugins share the exact Go toolchain and build environment.
- [ ] Native amd64 and arm64 images contain architecture-matching `.so` files.
- [ ] Every expected extender file is present; no required source asset is silently omitted.
- [ ] Experimental teamserver starts under the existing read-only, UID 10001, minimal-capability posture.
- [ ] Builder source is read-only and build scratch is ephemeral.
- [ ] Builders have no network access or teamserver-state mounts.
- [ ] Teamserver cannot execute native build tools.
- [ ] Builder absence produces a clear operator error without crashing the server.
- [ ] Builder identity mismatch is rejected.
- [ ] Requests and responses are bounded and validated.
- [ ] Sensitive configuration and generated artifacts do not appear in logs.
- [ ] Output files are non-empty, structurally valid, correctly typed, and hash-verified.
- [ ] Existing-volume profile behavior is documented and deliberately migrated.
- [ ] Trivy and SBOM checks cover every shipped image.
- [ ] Licenses and source provenance cover every redistributed component.

## Primary references

- [NaX repository at the reviewed commit](https://github.com/MaorSabag/NaX/tree/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3)
- [NaX setup script](https://github.com/MaorSabag/NaX/blob/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3/setup_nax.sh)
- [NaX runtime build callback](https://github.com/MaorSabag/NaX/blob/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3/src_server/agent_nonameax/pl_build_payload.go)
- [NaX agent module](https://github.com/MaorSabag/NaX/blob/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3/src_server/agent_nonameax/go.mod)
- [NaX arm64 `objcopy` pull request](https://github.com/MaorSabag/NaX/pull/2)
- [NaX MIT license](https://github.com/MaorSabag/NaX/blob/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3/LICENSE)
- [Official Go plugin warnings](https://pkg.go.dev/plugin#hdr-Warnings)
- `Dockerfile`
- `docker-compose.yml`
- `profile.kharon.yaml`
- `Kharon/setup_kharon.sh`
- `Kharon/agent_kharon/src_server/pl_agent.go`
