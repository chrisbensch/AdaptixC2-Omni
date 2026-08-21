# NaX Sidecar Prototype — Design

**Date:** 2026-08-21
**Status:** Approved design (Option A). Implementation plan follows.
**Scope:** Milestone 2 only — route `agent_nonameax` payload generation through a single builder worker over a Unix socket. Does **not** change the default image posture, does **not** touch Kharon, does **not** add hardening beyond prototype defaults.

---

## 1. Problem

`agent_nonameax.BuildPayload` performs **native Windows payload compilation inside the teamserver at request time**: it generates `Config.h`/`Config_profile.h`/`Config_sleepmask.h`, runs `make -C <NaX> …`, and (for `exe`/`dll`/`svc`) invokes `x86_64-w64-mingw32-g++`. To support this, the `runtime` Docker stage ships the full native toolchain (clang, nasm, make, binutils, both mingw toolchains, python3) plus the complete NaX source tree at `/app/NaX`, chown'd to the unprivileged UID so the server can write generated headers into the image on every build.

This is an anti-pattern that (a) ships an unreviewed source tree and a full cross-compiler into every exposed teamserver, (b) writes mutable generated artifacts into otherwise immutable image layers, and (c) forecloses restoring `read_only: true` (only achievable once *no* agent compiles in-server).

**Goal of this prototype:** move the native payload compile out of the teamserver into a dedicated builder worker, reached over a Unix socket, while leaving the server's in-process packing (`packNaxBin`) untouched.

## 2. Scope boundaries

| In scope (this prototype) | Out of scope (deferred) |
|---|---|
| `agent_nonameax` payload generation routed through the builder | The 3 listeners + `service_nax_store` (they compile nothing in-server) |
| Builder owns native compile + optional mingw PE wrapper | Kharon — left exactly as-is |
| Builder returns raw **components**; server repacks via existing `packNaxBin` | The full `BuildPayload` rewrite / other agents |
| One builder worker covering the single compiling agent | `read_only: true` restoration (step C) |
| Socket protocol + minimal worker + small builder image + compose wiring | Hardening (redaction, concurrency, limits tuning) — step B |

## 3. Architecture

- Teamserver container + new **`nax-builder`** container.
- They share a **tmpfs mount** at `/run/nax` that holds the Unix socket file `builder.sock`. A shared tmpfs is used precisely because Unix domain sockets cannot cross a container boundary without a shared filesystem mount.
- The **builder worker** is a tiny standalone Go program (not the server) that:
  - `net.Listen("unix", "/run/nax/builder.sock")`
  - Handles **one request at a time** (prototype; concurrency is step B)
  - Compiles in a fresh ephemeral workspace
  - Writes the framed response
- The **server patch** is confined to `agent_nonameax/pl_build_payload.go`: after parsing `BuildProfile`, build a `NaxBuildRequest`, dial the socket, write handshake + request, read the components response, then call the existing `packNaxBin(...)`.

### Data flow

```
operator → BuildPayload()
          ├─ parse BuildProfile → NaxBuildRequest
          ├─ Dial("unix", "/run/nax/builder.sock")
          ├─ write handshake {proto, nax_sha, patch_digest, toolchain_id}
          ├─ write request frame (components inputs)
          ├─ read response frame (loader, beacon, pdata, xdata, textRva, flags, stompDll, size, sha256)
          ├─ packNaxBin(loader, beacon, pdata, xdata, textRva, flags, stompDll)   // unchanged
          └─ return ([]byte, filename, error)

nax-builder container (network_mode: none):
          accept → ephemeral workspace → copy pinned source → generate Config.h → make … → read components → response
```

## 4. Socket path convention

`resolveNaxRoot()` already reads `ModuleDir/nax_root.conf` to locate the NaX source. Extend that same convention: add a sibling file `ModuleDir/nax_builder_socket` containing the absolute socket path (default `/run/nax/builder.sock`). If the file is absent, fall back to `/run/nax/builder.sock`. No new runtime write path — the file lives in the image.

Builder identity is verified against expected values in the handshake (see §5). A missing socket file → builder absent → clear operator error, no crash.

## 5. Protocol

### Transport
Length-prefixed JSON frames: 4-byte big-endian length + UTF-8 JSON body. One connection = one request/response. No shell fragments, no caller-provided paths/Make-targets — the request is fully server-derived.

### Handshake (first frame)
```json
{"proto": 1, "nax_sha": "…", "patch_digest": "sha256:…", "toolchain_id": "…"}
```
The server compares these against the values baked into the worker image. Any mismatch → close connection → server returns a clear error (`builder identity mismatch`). No request is processed.

### Request schema (component fields, all derived from `BuildProfile`)
```json
{
  "reqId": "…", "deadlineMs": 20000,
  "transport": "http" | "smb",
  "pipeName": "naxsmb",              // smb only
  "callbackHost": "…",               // http only
  "callbackPort": 443,               // http only
  "bootURI": "/api/v1/status",       // http only
  "ssl": true,                       // http only
  "sleepMs": 10000, "jitterPct": 30,
  "encKeyHex": "<32 hex chars>",
  "watermark": "0x…", "listenerWatermark": "0x…",
  "gate_apis": ["a","b"],
  "stomp": {"mode":1, "adv":0, "dll":"chakra.dll", "unwind":true, "threadPool":true}, // mode/adv 0/1 ints → build emits NAX_STOMP_MODE/…=1|0
  "bof": {"stomp":true, "dll":"chakra.dll", "pool":["jscript9.dll","mshtml.dll","d3d11.dll"]},
  "sleepObf": 1,
  "outputFormat": "bin" | "exe" | "dll" | "svc",
  "svcName": "NaxService", "dllExport": "Runner",
  "debug": false
}
```
Value sets (`transport`, `outputFormat`, dll names, stomp modes) are allowlisted; out-of-range values are rejected as malformed.

### Response schema (success)
```json
{
  "ok": true,
  "filename": "nax.x64.bin",
  "size": 12345,
  "sha256": "…",
  "components": {
    "loader": "<base64>",
    "beacon": "<base64>",
    "pdata":  "<base64>",
    "xdata":  "<base64>",
    "textRva": 2048,
    "flags":  "0x…",          // parsed back to uint32 by the server before packNaxBin
    "stompDll": "chakra.dll"
  }
}
```
### Response schema (failure)
```json
{"ok": false, "error": "<machine-readable string>"}
```
Response is size-bounded and field-checked; unexpected/missing fields → server treats as malformed output and returns a clear error.

### Bounds (prototype defaults, tightened in B)
- Max request bytes, max response bytes, per-request wall-clock `deadlineMs`.

## 6. Builder worker internals

Per request, in order:
1. Allocate a fresh ephemeral workspace (e.g. under the shared tmpfs, wiped after each request — see §8 cleanup).
2. Copy the **pinned** NaX source into the workspace (image source is read-only; the workspace is the only writable place).
3. Generate `Config.h` / `Config_profile.h` / `Config_sleepmask.h` from the request.
4. Run the exact `make …` args encoded in the request (`link-components` / `components` / debug variants), plus the optional sleepmask BOF build when `beacongate`, plus the mingw PE wrapper when `outputFormat != "bin"`.
5. Read the six components (loader, beacon, pdata, xdata, textRva, flags) and return them alongside the filename, size, and sha256.

The worker computes `flags`/`stompDll` exactly as the server would have, so `packNaxBin` produces identical output whether the compile ran in-server or in the builder.

### Builder image
Small image containing: the pinned NaX source (one commit), the prebuilt invariant `.o` files, a **pinned cross toolchain** (`x86_64-w64-mingw32-*`, `nasm`, `make`, `objcopy`), and the worker binary. Pinning `x86_64-w64-mingw32-objcopy` in the builder removes the native-arm64 `objcopy` blocker that currently breaks in-server compilation on arm64.

## 7. Server-side change

Only `agent_nonameax/pl_build_payload.go` (and the matching `.so` build step in the Dockerfile) change:

- After parsing `BuildProfile`, build `NaxBuildRequest`.
- Dial the socket (path from `nax_builder_socket` or fallback), write handshake + request frame, read response.
- On any failure (socket absent, handshake mismatch, timeout, malformed response) return a clear operator error and **do not crash** the server.
- On success, call the untouched `packNaxBin(...)` and return `([]byte, filename, error)`.

Nothing else in the server changes. The 3 listeners and `service_nax_store` are unaffected. Kharon is untouched.

## 8. Compose / Dockerfile changes

- New `nax-builder` service in the `runtime` compose profile.
  - Image: the builder image (built from the new `Dockerfile.nax-builder`).
  - `network_mode: none` (socket traffic stays local; no external exposure).
  - Reads/writes the shared tmpfs at `/run/nax`.
  - Runs as a dedicated non-root UID, read-only rootfs, `no-new-privileges`.
  - Resource limits: CPU / memory / PID / wall-clock timeout / scratch-size / output-size.
- Teamserver image **drops** the full `/app/NaX` source tree and the native toolchain (clang/nasm/make/binutils/mingw/python3) — those move into the builder image only.
- Shared tmpfs mount at `/run/nax` in both containers.

Per-request cleanup: the ephemeral workspace is destroyed after success, failure, timeout, or cancellation.

## 9. Exit criterion (Milestone 2)

- One deterministic end-to-end request succeeds **without any compiler or writable source tree in the teamserver**.
- Builder absence → clear operator error without crashing the server.
- Timeout, malformed input, and malformed output each produce a clear error.

## 10. Deferred

- **Step B (hardening):** log redaction, concurrency, tightened limits, response verification, experimental profile + CI matrix + SBOM/provenance, promotion decision.
- **Step C (lock down):** restore `read_only: true` on the teamserver — which forces a Kharon sidecar too.

## 11. References

- `NAX-INTEGRATION.md` (planning/decision record, milestone timeline)
- `NaX/src_server/agent_nonameax/pl_build_payload.go` (in-server compile entry point)
- `NaX/src_server/agent_nonameax/nax_packer.go` (in-process packing, unchanged)
- `AdaptixC2/AdaptixServer/core/server/ts_agent_builder.go`, `core/extender/ex_agent.go` (server build contract)
- Current `Dockerfile` (build-server + runtime stages), `docker-compose.yml` (runtime service)
