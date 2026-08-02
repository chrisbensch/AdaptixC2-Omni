# NaX Compatibility Spike Design

**Date:** 2026-08-02  
**Status:** Design approved during brainstorming; implementation not started

## Goal

Determine whether the pinned NaX revision can compile and load as four Adaptix plugins against the current AdaptixC2 snapshot on native Linux amd64 and arm64, without changing the supported Omni image or runtime posture.

The reviewed NaX revision is:

```text
bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3
```

## Scope and boundary

This is a compatibility spike, not production integration.

- Use a native Linux amd64 development host first.
- Repeat on a native Linux arm64 host after amd64 succeeds.
- Use a temporary, externally checked-out NaX source tree.
- Use a disposable copy of the pinned AdaptixC2 source tree.
- Do not add NaX as a submodule yet.
- Do not modify the supported root `Dockerfile`, `docker-compose.yml`, `profile.kharon.yaml`, CI workflow, or default runtime image.
- Do not enable NaX in the supported profile.
- Do not invoke NaX `BuildPayload` during this spike. Runtime native compilation is a separate design and security problem.
- Leave the repository working tree unchanged after the spike.

The spike passes only if all four plugins compile and load on both native architectures, with the required runtime assets inventoried. Otherwise it produces a concrete blocker report.

## Why Linux amd64 first

NaX's build path is Linux-native and depends on Go, Make, MinGW-w64, NASM, Python, and binutils. Native amd64 removes Docker/QEMU overhead and exercises the conventional x86-64 server/plugin and Windows x64 cross-build combination. It provides the clearest first signal for source, API, and Go plugin ABI compatibility.

Native arm64 remains mandatory. NaX processes Windows x64 artifacts, and host-target `objcopy` selection is a known arm64 risk. The arm64 lane must prove that the x86-64 MinGW binutils are selected explicitly.

## Compatibility target

NaX supplies four server-side modules:

- `agent_nonameax`
- `listener_nonameax_http`
- `listener_nonameax_smb`
- `service_nax_store`

The current Adaptix server declares `github.com/Adaptix-Framework/axc2 v1.2.0` and Go `1.25.4`. The reviewed NaX modules declare the same `axc2` version and Go floor. Successful loading still requires the server and every plugin to use the same exact Go toolchain, dependency graph, source revisions, and `GOEXPERIMENT=jsonv2,greenteagc` settings.

## Disposable workspace

Use a temporary workspace outside the repository:

```text
/tmp/adaptix-nax-spike/
  AdaptixC2/       pinned Adaptix copy
  NaX/             verified NaX checkout
  artifacts/       staged plugins and runtime assets
  reports/         build, module, loader, and inventory reports
```

The NaX checkout and Adaptix copy are source inputs only. The repository's submodule directories are never edited.

## Build architecture

1. Verify the NaX checkout resolves to the reviewed commit and record the source SHA.
2. Copy the pinned AdaptixC2 tree into the disposable workspace.
3. Create a temporary `go.work` containing:
   - the AdaptixServer module;
   - all existing Adaptix extender modules;
   - NaX's four Go modules.
4. Run `go work sync` with the server's Go version and `GOEXPERIMENT=jsonv2,greenteagc`.
5. Build AdaptixServer and the existing Adaptix extenders through the normal Adaptix build path.
6. Build each NaX plugin explicitly with `go build -buildmode=plugin`. Do not use NaX's deployment Makefiles because they assume a different `Server/` layout and hard-code relative output paths.
7. Stage each plugin into a temporary `dist/extenders/<name>/` directory:
   - plugin `.so`;
   - `config.yaml`;
   - `ax_config.axs`;
   - `pe_templates/` for the agent where required;
   - temporary `nax_root.conf` pointing to the verified checkout.
8. Create a temporary profile registering all four NaX extenders alongside the baseline extenders.
9. Start the disposable server and verify health plus plugin-load logs.
10. Capture exact toolchain, module, architecture, source-hash, staged-file, and loader evidence under `reports/`.

The temporary `nax_root.conf` is intentional test scaffolding. Its source-tree reference is not an acceptable production runtime design.

## Validation matrix

### Native amd64

- Build AdaptixServer and all existing extenders.
- Build all four NaX plugins.
- Run the NaX Go tests where present.
- Confirm server and plugins are x86-64 ELF files.
- Confirm the server and plugins share the exact Go toolchain and experiment settings.
- Start the disposable server with all four NaX profile entries enabled.
- Verify the TLS/HTTP health endpoint.
- Verify plugin-load logs contain no ABI, symbol, or initialization errors.
- Inventory each plugin's `.so`, config, AxScript, templates, and source-root metadata.

### Native arm64

Repeat the amd64 checks natively and additionally:

- Confirm server and plugins are AArch64 ELF files.
- Confirm Windows artifact processing uses `x86_64-w64-mingw32-objcopy`, never host-target `objcopy`.
- Run a minimal PE-object conversion or equivalent cross-tool artifact check.
- Fail the lane if a Makefile or helper silently chooses host-target binutils.

### Repository isolation

After each lane:

- The supported repository has no NaX source, profile entry, patch, generated artifact, or modified submodule content.
- The default image definition remains unchanged.
- No compiler or source tree is introduced into the supported runtime image.

## Exit criteria

The spike is successful only when all conditions hold on both native architectures:

1. AdaptixServer and all four NaX plugins compile in one Go workspace.
2. NaX tests pass, or each pre-existing failure is documented with command output and scope.
3. All four plugins load without Go plugin ABI, symbol, or API errors.
4. The temporary server starts with NaX profile entries enabled and passes its health check.
5. Every required runtime asset is present and recorded.
6. Architecture and cross-tool checks pass, including the arm64 x86-64 `objcopy` check.
7. No supported repository or runtime artifact changes are required.

A failure is classified as one of:

- Go ABI or toolchain mismatch;
- Adaptix API or source incompatibility;
- NaX build-layout or deployment assumption;
- native toolchain or cross-architecture failure;
- missing runtime asset or profile registration;
- plugin loader or runtime initialization failure;
- provenance or licensing blocker.

## Explicit non-goals

This spike does not:

- add NaX to the default image;
- design or implement a builder sidecar;
- permit runtime compilation inside the teamserver;
- make the read-only runtime writable;
- mount the Docker socket;
- add NaX commands to Extension-Kit or PostEx-Arsenal;
- validate payload execution on Windows;
- establish NaX provenance as sufficient for redistribution.

A successful spike authorizes a separate design phase for experimental integration. It does not authorize promotion to the supported image.

## Evidence to retain

Each architecture run should retain:

- NaX and Adaptix source SHAs;
- host/kernel architecture;
- `go version` and `GOEXPERIMENT`;
- `go.work` and synchronized module graph;
- compiler, NASM, MinGW, LLVM, and binutils versions;
- build commands and exit status;
- `file`/ELF architecture results;
- staged asset inventory with hashes;
- server health result;
- plugin loader logs;
- test results;
- any blocker classification and reproduction steps.

## References

- [`NAX-INTEGRATION.md`](../../../NAX-INTEGRATION.md)
- [NaX reviewed revision](https://github.com/MaorSabag/NaX/tree/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3)
- [NaX setup script](https://github.com/MaorSabag/NaX/blob/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3/setup_nax.sh)
- [NaX runtime payload callback](https://github.com/MaorSabag/NaX/blob/bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3/src_server/agent_nonameax/pl_build_payload.go)
- [Adaptix Go plugin warnings](https://pkg.go.dev/plugin#hdr-Warnings)
