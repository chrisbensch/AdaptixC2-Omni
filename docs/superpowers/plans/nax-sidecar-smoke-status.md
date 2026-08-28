# Nax Sidecar (Milestone 2) — Status & Tasks

Working session: confirm the builder sidecar produces a Nax payload **through the
socket** (no in-server `go build`) on Bookworm mingw-w64 12.2, with Kharon untouched.

> **Commit note:** the Milestone 2 work is committed (`nax-sidecar: fix
> loader/beacon build (objcopy toolchain + OK flag)`). The submodule trees stay
> clean — nothing is committed inside them. Local-only dev state (below) is
> reproducible via `scripts/setup-local.sh`, not committed.

**Local-only dev state (not committed, per AGENTS.md):** the `AdaptixC2`
submodule's `go.work` needs two extra `use` entries (the pinned NaX agent module
+ the sidecar) so `go vet`/`go test`/`go build` resolve the agent→sidecar local
`replace`. These are **layout-specific** — locally `../../NaX/...` /
`../../sidecar/...`, but the Docker build strips and re-adds them with
correct container paths — so they can't be a single committed patch and must
stay out of the submodule. Reproduce them with:

```bash
./scripts/setup-local.sh   # applies patches/adaptixc2-go-work.patch; idempotent
```

---

## Checklist

### ✅ Done & verified (uncommitted)
- [x] **Mingw-w64 12.2 loader build fix** — `__faststorefence`/`-mno-sse` break in
      `src_loader` (C++/g++). Pre-define the guard before `<windows.h>` in
      `src_loader/include/Common.h`. Captured as a patch (repo convention).
- [x] **Patch wired into both Dockerfiles** — `Dockerfile` (build-server stage) +
      `Dockerfile.nax-builder` (runtime stage, plus added `patch` to apt install).
- [x] **Builder image builds EXIT=0**; loader compiles cleanly (8 objects, 0 sfence
      errors) with the patch baked in.
- [x] **Worker header-writing gap closed** — `writeConfigHeaders()` in `build.go`,
      wired into `handleRequest()` after `Validate()`. Only non-empty headers are
      written; a tree's own pinned copy is left alone.
- [x] **Unit test** `TestWriteConfigHeaders` added; `go vet ./...` clean,
      `go test ./...` passes.

### 🔧 In progress — now resolved
- [x] **Smoke client compiles** — fixed `cmd/smoke/main.go:97` (`WriteString`
      → `fmt.Fprintf`).
- [x] **Second smoke bug found & fixed** — `genConfigProfileH` emitted a
      **malformed** header (one `(p)[i]` per line with the line-continuation `\`
      in the wrong place), which the compiler rejected as "conflicting types for
      'p'". The server's real format is `writeBytesWriteMacro` (perLine=8,
      fieldWidth=3): assignments joined on lines ending in ` \`. Rewrote
      `genConfigProfileH` to match `NaX/src_server/agent_nonameax/pl_build.go`
      exactly. Verified the generated header now compiles.

### ✅ Smoke test — end-to-end run (uncommitted work, verified live)
Rebuilt the builder image (`:patched`) with the mingw patch baked in, ran it on a
fresh `nax-sock` volume, and drove it with the cross-compiled `nax-smoke` client
over `/run/nax/builder.sock`. Proven working:

- [x] **Socket comms** — client ↔ builder request/response round-trips cleanly.
- [x] **writeConfigHeaders fires with real data** — debug confirmed the builder
      received `cfgH=2874 profH=27479 sleepH=54174` and wrote a valid
      `Config_profile.h` (27479 bytes, 215 lines) into its own `/nax` tree.
- [x] **Loader + beacon COMPILE** — the faststorefence fix holds; all loader and
      beacon translation units compile with 0 sfence errors.

### ✅ Resolved — `section below image base` / objcopy (was thought out-of-scope)
At the **PE-exe link / objcopy** stage both the loader and beacon fail:

```
x86_64-w64-mingw32-ld: bin/nax_loader.x64.exe:.text: section below image base
objcopy: build/http/beacon.x64.exe: file format not recognized  → 0-byte .bin
```

**Root cause (fixed in `6bcee2e`):** the loader/beacon Makefiles used
`OBJCOPY := objcopy`, which resolves on this image to the host default — an **ARM**
triple (`aarch64-linux-gnu-objcopy`) that cannot read x86-64 PE files. The
`section below image base` line is a harmless ld *warning* (PE sections land at
`image_base + 0x1000`); the real failure is `objcopy: file format not recognized`
→ 0-byte `.bin`. Proven independent of the sidecar: `make loader` (loader-only, no
generated header) fails identically.

**Fix:** point both `src_loader/Makefile` and `src_beacon/Makefile` at
`x86_64-w64-mingw32-objcopy` (supports `pe-x86-64`) via
`patches/nax-loader-objcopy.patch` + `patches/nax-beacon-objcopy.patch`, applied in
`Dockerfile.nax-builder`. Verified: smoke run now produces valid components (loader
2976 B, beacon 91007 B). No submodule change needed — this was a toolchain path
issue, not a `Linker.ld` problem. The sidecar's job — socket + header writing +
invoking the real `make` path to produce a Nax payload — is fully proven.

### ⏭️ Next (when resuming Milestone 2)
- [ ] **Lock in the objcopy fix** — re-run `nax-smoke` against a freshly built
      committed image to confirm valid components (loader/beacon `.bin`) on the
      `:patched` image, not just a locally-built one. (The socket-payload goal is
      met; this closes out the stale blocker line above.)
- [ ] **CI parity** — ensure the objcopy patch + header-writing land in CI so they
      are verified on amd64 + arm64 (CI already runs both arches). Currently the
      fix is only validated locally.
- [x] **`read_only` re-evaluation** — RESOLVED. Dropped because the Kharon agent
      compiles beacon/loader at runtime (writes into the rootfs). Nax compiles in
      the nax-builder sidecar instead, so a **Nax-only deployment can run with
      `read_only: true`** — every runtime write (SQLite DB, listener data,
      downloads, screenshots, TLS cert/key) goes to the writable `./data`
      bind-mount at `/app/data`, and logs go to stdout; no tmpfs needed. Default
      stays `false` (Kharon is in the shipped profile). Example + toggle:
      `profile.nax-only.yaml` and `read_only: ${ADAPTIX_READ_ONLY:-false}` in
      `docker-compose.yml`.

### ⛔ Blocked / not feasible now
- [ ] **True end-to-end with a real profile** — no NaX C2 profile fixture exists
      (`Config_profile.h` needs ~1697 serialized bytes with a real wire format; it's
      gitignored and generated per-build by the server). Can't hand-craft a valid one.
- [ ] **Full server image not built** (only `build-server` target) — no running
      teamserver to drive through.

### 🔮 Later passes (not started)
- [ ] **Milestone 3** — move the Kharon agent over to the builder sidecar too.
- [ ] **CI parity** — confirm the new patch + header-writing land in CI (already
      verifies amd64 + arm64).

---

## Context / decisions (don't re-derive)

### Mingw fix — root cause
`src_loader` is C++/g++ and built with `-mno-sse`. Bookworm's mingw-w64 12.2
`intrin-impl.h` defines `__faststorefence()` via `__builtin_ia32_sfence()`, which
is unavailable under `-mno-sse`. `src_beacon` (C/gcc) is unaffected because it
includes `Macros.h` (with the guard) *before* `windows.h`; the loader's
`Common.h` pulls `<windows.h>` first, so its own guard arrives too late.

### Header-writing gap
The request carries `ConfigH`/`ConfigProfileH`/`ConfigSleepmaskH` (base64) with a
comment that "the sidecar just writes them into its copy of the NaX source" — but
`BuildComponents` never did. The beacon won't compile without those headers present
in `src_beacon/include/`.

### Not-feasible details
- No real NaX C2 profile fixture (see above).
- Full server image not built; only `build-server` target present.

### Open design question (when resuming)
Should the worker own writing those generated headers (current approach), or is that
meant to happen elsewhere?
