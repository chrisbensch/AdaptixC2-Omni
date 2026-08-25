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
### This session (Dockerfile/compose wiring + patch approach)
- **Decision: NaX submodule changes go in `patches/`, NOT committed inside the
  submodule.** Repo rule (AGENTS.md): don't commit inside submodule trees;
  persistent customizations go in `patches/` or Dockerfile-side at COPY time.
  So the 4 NaX files in `NaX/src_server/agent_nonameax/` (`go.mod`,
  `pl_build_payload.go`, new `pl_build_payload_test.go`, new `pl_nax_sidecar.go`)
  are captured as a **patch**, applied at build time. The NaX submodule working
  tree is restored to its pinned SHA (clean) — the changes live only in the patch.
- **Patch:** `patches/adaptix-nax-sidecar-task6.patch` — regenerated with
  **FULL paths relative to the NaX submodule root** (`src_server/agent_nonameax/...`),
  matching repo convention (kharon/go-dependencies patches do the same). It
  forward-applies clean.
- **Patch application (Dockerfile, build-server stage):** applied AFTER
  `COPY NaX/src_server/agent_nonameax /src/AdaptixC2/AdaptixServer/extenders/agent_nonameax`
  and BEFORE `go work sync`, so the patched go.mod's sidecar `replace` is picked
  up by `go work sync`. Uses **`patch -p2`** (not `git apply`) from cwd
  `/src/AdaptixC2/AdaptixServer/extenders` — this is the repo convention
  (kharon uses `patch -p2`). `-p2` strips the `a/` prefix + `src_server/`, landing
  files in `extenders/agent_nonameax/`. NOTE: `-p1` from the parent dir and
  `patch -p2` from *inside* agent_nonameax both mis-strip — verified by hand.
- **Dockerfile changes (committed `d36bff6`):**
  - `COPY sidecar /src/AdaptixC2/sidecar` (the sidecar source the agent's
    `replace` resolves to).
  - `go work use ... ../sidecar/nax-builder` — **path is `../sidecar/...`, NOT
    `../../sidecar/...`**. cwd is `/src/AdaptixC2/AdaptixServer`; the sidecar is
    at `/src/AdaptixC2/sidecar/nax-builder` (one `..`, not two). This was a bug
    I introduced and fixed this session.
  - `groupadd naxb` (GID 10002) + `usermod -a -G naxb adaptix` so the server
    (UID 10001) can dial the builder's socket.
  - Removed the server image shipping `/app/NaX` source tree (now only in the
    builder image).
- **Compose fix (`docker-compose.yml`, committed `f971492`):** the two
  per-container `/run/nax` **tmpfs** mounts were private — the socket the
  builder wrote was never visible to the server. Replaced with a shared **named
  volume `nax-sock`** mounted at `/run/nax` in both `server` and `nax-builder`
  services. Added top-level `volumes: nax-sock:`.
- **Builder socket mode (`sidecar/nax-builder/naxbuilder/worker.go`, committed
  `f971492`):** `net.Listen` creates the socket `0o755` (no group write), which
  blocks connect() for the non-owner server. Added a best-effort `os.Chmod(sock,
  0o660)` after bind so the server (in the `naxb` group) can connect.
- **Current blocker (Aug 24): `go work sync` fails in the container** even after
  the path fix. Real build (not `--check`) of `build-server` dies at step 14/24:
  `go: open /src/NaX/src_server/agent_nonameax/go.mod: no such file or directory`
  and `go: open /src/sidecar/nax-builder/go.mod: no such file or directory`. The
  `go work use ./extenders/agent_nonameax` + `../sidecar/nax-builder` line is
  resolving to paths that don't exist in the container. **Not yet diagnosed** —
  likely a `replace` directive in AdaptixC2's go.mod redirecting the module path,
  or the sidecar/agent aren't where `go work use` expects. The patch itself
  applies cleanly (verified: all 4 files). See `docker build --no-cache
  --target build-server .` to reproduce.
- **Now on (re-verified green, Aug 23):** **Task 6 + Task 1 + Task 2 done** —
  - `351d675`: the server-side change `pl_nax_sidecar.go` + the inlined
    request in `pl_build_payload.go` + the `bool` request + 2 tests, all
    passing; the sidecar `replace` is pinned in the submodule's `go.mod`.
    The working patch `patches/adaptix-nax-sidecar-task6.patch` is **done**
    (512 lines, 4 files, forward-applies clean, Kharon intact).
  - `7f8baaa`: **the builder module scaffold** — `sidecar/nax-builder/`
    `go.mod` + `go.sum` + the `naxbuilder/` files (`request`, `frame`,
    `pe`, `build`, `worker` + tests) + `cmd/nax-builder/main.go`. Build is
    green (4.1M), `TestComponentPath` pass, vet clean.
  - **Task 2**: **the framing** (4-byte big-endian length + JSON, `MaxFrameBytes`
    = `256<<10` cap, the "frame too big" + "short read" errors). `TestFrameRoundTrip`
    + `TestFrameTooLargeRejected` both pass, vet clean, build green.
  - **Aug 23 re-check (live, on this machine):** sidecar `go vet ./...` clean,
    `go test ./...` pass in **0.386s**, binary builds green at **4.1M**. NaX's
    4 files in `src_server/agent_nonameax/` are **uncommitted** (the `replace`
    is in its `go.mod`); the workspace sits **14 commits ahead of origin/main**
    (unpushed). No container running right now — OrbStack daemon is up.
- **Next:** (2) **Dockerfile + compose** (the builder stage: pinned NaX +
  toolchain, its own socket + tmpfs), (3) the **smoke test** (build + run +
  a build through the socket).
  - (1) — the sidecar `go test` + `go build` — is **done** (Aug 23, all green).
  - Nax **wired into the AdaptixServer workspace**: `AdaptixC2/AdaptixServer/go.work`
    now lists `../../NaX/src_server/agent_nonameax` + `../../sidecar/nax-builder`.
    Verified: whole-workspace `go build ./...` green; Nax agent module `go test ./...`
    pass (0.40s); sidecar `go test ./...` pass; 0 vet issues from Nax/sidecar (the
    42 warnings are pre-existing upstream `core/` format-string checks).
  - **Dockerfile fixed** (was a latent break): the Nax agent's `go.mod` resolves the
    sidecar via `replace ... => .../sidecar/nax-builder`, which inside the container
    is `/src/AdaptixC2/sidecar/nax-builder` — previously never copied. Added
    `COPY sidecar /src/AdaptixC2/sidecar` + wired `../../sidecar/nax-builder` into
    the `go work use` line. `docker build --check --target build-server` → clean
    (EXIT 0).

## Handful of constants
- Submodule (AdaptixC2): **bd032d9a**; Nax: **45c5114**; the agent
  extender is `NaX/src_server/agent_nonameax/`.
- **The builder's socket:** `nax_builder_socket` (default
  `/run/nax/builder.sock`), in `nax_root.conf` next to the loader path.
- **The pinned replace** (in the submodule's `go.mod`):
  `replace .../sidecar/nax-builder => ../../../sidecar/nax-builder`.
- **The build recipe** (the per-extender Makefile): the
  `-buildmode=plugin` `ldflags -s -w`.
- **The root socket dir is a shared named volume** (`nax-sock`, mounted at
  `/run/nax`) — NOT a tmpfs. Two per-container tmpfs mounts are private, so the
  socket the builder wrote was never visible to the server. The builder writes
  its unix socket there (chmod'd `0o660` so the server, in the `naxb` group,
  can connect); the server dials it. See `docker-compose.yml` +
  `sidecar/nax-builder/naxbuilder/worker.go`.
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
- [x] A unit test or two on the new pieces. (frame / pe / build / worker
      in the sidecar + the 110-line `pl_build_payload_test.go` in NaX)
- [ ] The **smoke test** (the run-side check) — the server + builder
  live, and a build goes through the socket.
- [ ] The `data/` dir is back to its write-ability (the
  `read_only` decision, evaluated in a later pass).
