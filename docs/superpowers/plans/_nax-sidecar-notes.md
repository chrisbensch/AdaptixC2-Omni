# Nax Sidecar (Prototype) — Standing Notes

Living note for the Nax sidecar prototype (the "Task 6" work: the
server-side .so agent calls a separate builder process over a unix socket
instead of compiling in-process). This survives across sessions — the
_implementation_ goes in the plan (above), the _durable_ context lives here.

## What we're doing
- Moving Nax agent payload generation out of the teamserver process into a
  small separate builder. The server .so (agent_nonameax) sends a build
  request over a unix socket; the builder compiles the bof + stomp dll
  and hands back the raw components; the server repacks them with its
  existing packer. This is the "second milestone" of the Nax integration
  (see the design spec).
- Small, additive build. Kharon stays exactly as it is; the server's
  `read_only` stays dropped for the prototype (we'll evaluate re-adding it
  in a later pass).

## Durable decisions (so you don't re-decide)
- **Server repacks.** The builder hands back raw components; the server
  calls its existing `packNaxBin` unchanged.
- **Pinned builder image.** The builder runs in its own small image
  (pinned NaX source + prebuilt `.o` + pinned cross-toolchain) rather than
  in the big server image.
- **Existing sidecar API, side by side.** The Kharon agent keeps its
  in-server make; only the Nax agent goes through the builder, so we
  don't touch the existing agent flow.
- **Minimal change.** The goal is the smallest additive step — one
  builder worker that takes a request, builds, and returns the
  components.
- **Kharon untouched.** Its build path is its own; this only adds the
  Nax builder.
- **Defaults stay put in the prototype.** `read_only` stays dropped; we'll
  evaluate restoring it in a later pass.
- **Builder isolation.** The builder runs in its own image: `network_mode:
  none`, a non-root UID, read-only rootfs, and a fresh ephemeral
  workspace per build.
- **`stomp.dll` is additive.** It's the one compiled output that lands
  in the dist; the raw components are the sidecar's.
- **Socket path is a config item.** The builder's socket lives in the
  existing `nax_root.conf` (next to the loader path) with a clear
  "builder not up yet" error if it's missing.
- **`StompMode`/`StompAdv` are bool** on the request, so the server's
  existing bool profile fields map straight across.
- **`buildViaSidecar(req)`** — the request is built inline from the
  server's existing local fields; no extra helper.

## Fiddly bits (the "why" that's easy to forget)
- **The `naXSha` + `patchDigest` in the handshake** let the server and
  builder confirm they're in sync.
- **`readTextRVA`/`writeTextRVA`** re-parse the rva string (e.g. "4096"
  in the Makefile) as **hex** — so the wire value is the Makefile
  number, and the test asserts `0x4096`.
- **Windows TCP callback** is the http-port stand-in (127.0.0.1:4321).
- **The front-runner 404 stays**; `stomp.dll` is additive.
- **`GOEXPERIMENT=jsonv2,greenteagc`** for the Go builds.
- **The bd032d9a layout (the gotcha):** the submodule tree keeps its
  agent files **at the submodule root** (`go.mod`, `pl_build_payload.go`,
  ...), while the submodule's HEAD *commit* (bd032d9a) keeps them
  **nested** under `src_server/agent_nonameax/`. So a patch made
  **at the root** (the flat paths) is the one that forward-applies clean.
- **The `replace` is 3 levels up.** The pinned
  `replace .../sidecar/nax-builder => ../../../sidecar/nax-builder`
  (not 2) — the agent dir is two levels deep, the sidecar is one out.
- **macOS `sun_path` is 104.** The test's socket path stays short
  (`/tmp/nax_sidecar_test.sock`) to dodge the 104-char cap.
- **Builder-absent is a clear Error.** No builder in the unit tests →
  the "builder not up yet" message (not a crash).

## If I need a fresh session
- **Now on:** **Task 6 is built + tested** (the server-side change
  `pl_nax_sidecar.go` + the inlined request in `pl_build_payload.go` +
  the `bool` request + 2 tests, all passing; the sidecar replace is
  pinned in the submodule's `go.mod`). The working patch
  `patches/adaptix-nax-sidecar-task6.patch` is **done** (512 lines,
  4 files, forward-applies clean, Kharon intact).
- **Next:** (1) **commit** the patch + these notes (small), (2) **Tasks
  1–8** — the sidecar module (`naxbuilder/` request → frame → pe →
  build → worker, + `cmd/nax-builder/main.go`), (3) **Dockerfile +
  compose** (the builder stage: pinned NaX + toolchain, its own
  socket + tmpfs), (4) the **smoke test** (build + run + a build
  through the socket).

## Handful of constants
- Submodule (AdaptixC2): **bd032d9a**; Nax: **45c5114**; the agent
  extender is `NaX/src_server/agent_nonameax/`.
- **The builder's socket:** `nax_builder_socket` (default
  `/run/nax/builder.sock`), in `nax_root.conf` next to the loader path.
- **The pinned replace** (in the submodule's `go.mod`):
  `replace .../sidecar/nax-builder => ../../../sidecar/nax-builder`.
- **The build recipe** (the per-extender Makefile): the
  `-buildmode=plugin` `ldflags -s -w`.
- **The root tmpfs is shared** (`/run/nax`) — the builder's fresh
  workspace + the socket both live there.
- **The build server image** (for the smoke run): the same
  `golang:1.25.12-bookworm` pin.
- **The protocol:** the 4-byte big-endian length + JSON body;
  `{ok, filename, size, sha256, components{loader, beacon, pdata,
  xdata, textRva, flags, stompDll}}` back.

## When this is done
- [ ] The full build runs green (host-arch or the CI platforms).
- [ ] The teamserver **and** the builder are running, **and** the
  `Read` endpoint answers.
- [ ] A Nax agent's payload builds **through the builder** (no
  in-server `go build`), and **Kharon stays in its own build** —
  side by side.
- [ ] A unit test or two on the new pieces.
- [ ] The **smoke test** (the run-side check) — the server + builder
  live, and a build goes through the socket.
- [ ] The `data/` dir is back to its write-ability (the
  `read_only` decision, evaluated in a later pass).
