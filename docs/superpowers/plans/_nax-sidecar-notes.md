# NaX Sidecar — Standing Context

> **RESUME POINT:** Next task is **Task 1** in `docs/superpowers/plans/2026-08-21-nax-sidecar-prototype.md` (builder-module scaffold + request/response types). First command:
> `cd sidecar/nax-builder && go mod init github.com/entropy-z/AdaptixC2-Omni/sidecar/nax-builder` (create it first if the dir doesn't exist), then write `naxbuilder/request.go` + `naxbuilder/request_test.go` per the plan, run `go test ./naxbuilder/ -run TestComponentPath -v`, commit.
> Then advance through Tasks 2–8; end with the Docker smoke test `scripts/smoke-nax-sidecar.sh`.

## NaX Sidecar — Context

Quick-reference for the NaX sidecar work (Option A / Milestone 2). Kept here so it
doesn't have to be re-derived every session. Last updated 2026-08-21.

## Where things live
- Design spec: `docs/superpowers/specs/2026-08-21-nax-sidecar-prototype-design.md`
- Implementation plan: `docs/superpowers/plans/2026-08-21-nax-sidecar-prototype.md`
- NaX submodule (pinned commit `bd032d9a…`): `NaX/` — server modules under `NaX/src_server/`.
- The single compiling agent: `NaX/src_server/agent_nonameax/` (`pl_build_payload.go` in particular).

## The core split (why it's cheap)
- **Server keeps (pure Go, no source tree):** the `generateConfigH*/generateProfileH/generateSleepmaskH/generateShellcodeH` helpers (`pl_build.go`) and `packNaxBin(...)` (`nax_packer.go`). Both just manipulate bytes / strings.
- **Moves to the sidecar:** only `make <target>` execution + reading the 6 component files + the mingw PE wrapper (`compileWrapper` needs `pe_templates/`).

## Key facts (facts that are easy to forget)
- The 3 listeners + `service_nax_store` don't compile in-server — only `agent_nonameax` does.
- The builder worker returns raw **components** (`loader/beacon/pdata/xdata/textRva`); the server repacks with the *unchanged* `packNaxBin(...)`. So `nax_packer.go` stays untouched.
- Component output paths (from `NaX/Makefile`):
  - loader:  `src_loader/bin/nax_loader.x64.bin`
  - http:    `src_beacon/build/http/beacon.{x64,pdata,xdata}.bin` + `.text_rva`
  - smb:     `src_beacon/build/smb/beacon.{x64,pdata,xdata}.bin` + `.text_rva`
  - debug:   `src_beacon/build/http/beacon.x64.debug.bin` + `.debug.{pdata,xdata}.bin` + `.debug.text_rva`
  - sleepmask (beacongate only): `src_sleepmask/dist/sleepmask.x64.o`
- `BuildPayload(BuildProfile, [][]byte) ([]byte, string, error)` is synchronous — the sidecar call must fit inside it.
- `BuildProfile` carries only: `BuilderId` (string), `AgentConfig` (JSON string), `ListenerProfiles[]` (`TransportProfile{Watermark, Profile}`).
- `NaxBuildRequest` field names / `ComponentPath` logic mirror `NaX/Makefile` exactly — keep them in lockstep.
- `resolveNaxRoot()` reads `ModuleDir/nax_root.conf`; the socket path convention adds a sibling `nax_builder_socket` file (fallback `/run/nax/builder.sock`).

## Tricky decisions / gotchas
- **Kharon is left exactly as-is during A.** It still compiles in-server, which is *why* the native toolchain must stay in the server runtime image during A and `read_only: true` stays OFF until step C. Don't remove the toolchain from the server image yet.
- **Socket transport:** Unix domain socket over a tmpfs mount shared between the `server` and `nax-builder` containers (`/run/nax/builder.sock`). Unix sockets can't cross a container boundary without a shared mount.
- **Framing:** 4-byte big-endian length + UTF-8 JSON body (`WriteFrame`/`ReadFrame`). Per-request `MaxFrameBytes`.
- **Handshake first frame:** `{proto, nax_sha, patch_digest, toolchain_id}` — server validates identity; mismatch => clear error, no request processed.
- `ServeConn` must `return nil` on a successful exchange (the error is carried in the response body, not the return value).
- `NaxBuildResponse` has NO `Error` field — the client returns a Go error directly on the error frame; `buildViaSidecar` checks `resp.OK` and returns a generic error otherwise.
- The builder path (`pl_build_payload.go` tail: `writeIfChanged` + `make` + file-read) is replaced by an inline `&naxbuilder.NaxBuildRequest{...}` built from the *existing local variables* that function already computes (`callbackHost`, `sleepMs`, `listenerSSL`, `transportProfile`, `svcName`, …) — no new parse helpers.

## Execution reminders (for me / future sessions)
- `edit`/`write`/`read`/`bash` all require a `path` param (for edit/write). Don't omit it.
- Matching huge code blocks in `edit` oldText is fragile — prefer reading the exact range first, or rewrite the whole file.
- Host is macOS arm64; the end-to-end Docker smoke test runs the real build (needs mingw). Pure-Go unit tests run on the host; mingw is skipped when absent.
