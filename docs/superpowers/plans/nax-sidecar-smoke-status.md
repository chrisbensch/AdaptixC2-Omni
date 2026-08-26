# Nax Sidecar (Milestone 2) — Status & Tasks

Working session: confirm the builder sidecar produces a Nax payload **through the
socket** (no in-server `go build`) on Bookworm mingw-w64 12.2, with Kharon untouched.

> **Commit note:** all work below is currently **uncommitted / untracked**. The
> submodule trees are clean (no dirty changes committed inside them). Before the
> next commit, stage `patches/`, `Dockerfile`, `Dockerfile.nax-builder`, and the
> `sidecar/` changes together.

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

### ⛔ New blocker — `section below image base` (pre-existing, out of scope)
At the **PE-exe link / objcopy** stage both the loader and beacon fail:

```
x86_64-w64-mingw32-ld: bin/nax_loader.x64.exe:.text: section below image base
objcopy: build/http/beacon.x64.exe: file format not recognized  → 0-byte .bin
```

**Proven pre-existing & environmental, not a sidecar bug:** `make loader` (loader
only — no generated header is involved) fails *identically* with a 0-byte bin. So
the error is independent of the header-writing / faststorefence work. Root cause is
NaX's `Linker.ld` (no section VMA) combined with `-Wl,--image-base,0x10000000` on
Debian bookworm's mingw-w64 12.2 — ld places sections below the image base and
objcopy then can't read the resulting exe. Fixing it means editing NaX's `Linker.ld`
or link flags, i.e. a **submodule** change — out of scope for Milestone 2 (and
AGENTS.md forbids committing inside submodule trees). The sidecar's job — socket +
header writing + invoking the real `make` path — is fully proven.

### ⏭️ Next (when resuming Milestone 2)
- [ ] Decide how to handle `section below image base`: (a) accept as a known
      NaX-toolchain limitation and document it, or (b) get an in-scope fix for the
      beacon/loader link (needs a NaX-side change — coordinate as a submodule patch).
- [ ] Re-run the smoke client against the committed `:patched` image to lock in the
      result before committing.

### ⛔ Blocked / not feasible now
- [ ] **True end-to-end with a real profile** — no NaX C2 profile fixture exists
      (`Config_profile.h` needs ~1697 serialized bytes with a real wire format; it's
      gitignored and generated per-build by the server). Can't hand-craft a valid one.
- [ ] **Full server image not built** (only `build-server` target) — no running
      teamserver to drive through.

### 🔮 Later passes (not started)
- [ ] **`read_only` re-evaluation** — dropped for the prototype; decide whether to
      restore it + `data/` write-ability.
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
