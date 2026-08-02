# NaX Compatibility Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and load the pinned NaX Go plugins against the pinned AdaptixC2 server on native Linux amd64 and arm64 without changing the supported Omni runtime.

**Architecture:** Execute the spike in `/tmp/adaptix-nax-spike-<arch>` on each native Linux host. Copy the pinned Adaptix tree and check out NaX at the reviewed SHA, create a temporary Go workspace that includes the four NaX modules, build NaX plugins explicitly, stage their runtime assets into a disposable Adaptix `dist`, and start the server with a temporary profile. No NaX source or generated build output enters this repository or the supported image.

**Tech Stack:** Go 1.25.x with `GOEXPERIMENT=jsonv2,greenteagc`, Go plugins, Make, MinGW-w64, NASM, Python 3, GNU binutils, AdaptixC2 profile/TLS bootstrap, native Linux amd64 and arm64.

## Global Constraints

- Use NaX commit `bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3`.
- Use the currently pinned `AdaptixC2/` submodule source as the server baseline.
- Use a native Linux amd64 host first, then a native Linux arm64 host.
- Keep NaX outside the repository as a temporary checkout.
- Build the server and every plugin with the same exact Go toolchain, module graph, source revisions, and `GOEXPERIMENT=jsonv2,greenteagc`.
- Do not modify the supported root `Dockerfile`, `docker-compose.yml`, `profile.kharon.yaml`, CI workflow, or default runtime image.
- Do not add NaX as a submodule or register it in a committed profile.
- Do not invoke NaX `BuildPayload`; runtime native compilation is outside this spike.
- Do not use NaX's hard-coded deployment Makefiles; build each plugin explicitly and stage its files into the disposable `dist` tree.
- On arm64, require `x86_64-w64-mingw32-objcopy` for Windows x64 artifact processing and reject host-target `objcopy`.
- Retain source hashes, toolchain versions, module graph, build output, staged-file hashes, health results, loader logs, tests, and blocker evidence under the temporary workspace's `reports/` directory.
- Preserve unrelated user changes. The current workspace already contains an untracked `NAX-INTEGRATION.md` and an untracked `AdaptixC2/AdaptixClient/Resources/AdaptixClient.icns`; do not stage, modify, or delete either.

## File and artifact map

The spike intentionally has no production source-file modifications.

- Read: `AdaptixC2/Makefile` — baseline server/extender build orchestration.
- Read: `AdaptixC2/AdaptixServer/go.work` — existing Go workspace modules.
- Read: `AdaptixC2/AdaptixServer/profile.yaml` — temporary profile template.
- Read: `patches/adaptixserver-go-dependencies.patch` — workspace build dependency patch.
- Read: `NAX-INTEGRATION.md` — prior findings and reviewed NaX SHA.
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/` — disposable Adaptix source/build copy.
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/NaX/` — verified NaX checkout.
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/artifacts/` — staged plugin dist and runtime assets.
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/` — evidence and blocker report.
- Create: `docs/superpowers/plans/2026-08-02-nax-compatibility-spike.md` — this plan only.

---

### Task 1: Prepare and attest the native host

**Files:**
- Read: `AdaptixC2/`
- Read: `patches/adaptixserver-go-dependencies.patch`
- Read: `NAX-INTEGRATION.md`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/host.txt`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/source-shas.txt`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/NaX/`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/`

**Interfaces:**
- Consumes: the workspace's pinned AdaptixC2 submodule and the public NaX repository.
- Produces: a verified temporary source workspace and host/toolchain attestation consumed by Tasks 2–5.

- [ ] **Step 1: Require a native Linux host**

Run on the dev box:

```bash
set -euo pipefail
[ "$(uname -s)" = "Linux" ]
arch="$(uname -m)"
case "$arch" in
  x86_64) spike_arch=amd64 ;;
  aarch64|arm64) spike_arch=arm64 ;;
  *) printf 'unsupported host architecture: %s\n' "$arch" >&2; exit 1 ;;
esac
printf 'spike_arch=%s\n' "$spike_arch"
```

Expected: `spike_arch=amd64` on the first host and `spike_arch=arm64` on the second. Do not use QEMU for the evidence-producing run.

- [ ] **Step 2: Create the isolated workspace**

```bash
set -euo pipefail
REPO_ROOT="$PWD"
SPIKE="/tmp/adaptix-nax-spike-${spike_arch}"
rm -rf "$SPIKE"
mkdir -p "$SPIKE"/{artifacts,config,reports}
printf '%s\n' "$SPIKE" > "$SPIKE/reports/workspace.txt"
```

- [ ] **Step 3: Capture host and toolchain identity**

```bash
set -euo pipefail
{
  printf 'date='; date -u +%Y-%m-%dT%H:%M:%SZ
  printf 'kernel='; uname -a
  printf 'arch='; uname -m
  printf 'go='; go version
  printf 'go_experiment=%s\n' "${GOEXPERIMENT:-jsonv2,greenteagc}"
  printf 'gcc='; x86_64-w64-mingw32-gcc --version | python3 -c 'import sys; print(sys.stdin.readline().strip())'
  printf 'g++='; x86_64-w64-mingw32-g++ --version | python3 -c 'import sys; print(sys.stdin.readline().strip())'
  printf 'objcopy='; x86_64-w64-mingw32-objcopy --version | python3 -c 'import sys; print(sys.stdin.readline().strip())'
  printf 'nasm='; nasm --version
  printf 'python='; python3 --version
  printf 'make='; make --version | python3 -c 'import sys; print(sys.stdin.readline().strip())'
} | tee "$SPIKE/reports/host.txt"
```

Expected: every command succeeds; the MinGW tools target `x86_64-w64-mingw32`; Go is the selected 1.25.x toolchain; `GOEXPERIMENT` is `jsonv2,greenteagc`.

- [ ] **Step 4: Record source SHAs before copying**

```bash
set -euo pipefail
printf 'adaptix_sha=%s\n' "$(git -C "$REPO_ROOT/AdaptixC2" rev-parse HEAD)" | tee "$SPIKE/reports/source-shas.txt"
printf 'patch_sha256=%s\n' "$(sha256sum "$REPO_ROOT/patches/adaptixserver-go-dependencies.patch" | python3 -c 'import sys; print(sys.stdin.read().split()[0])')" | tee -a "$SPIKE/reports/source-shas.txt"
```

- [ ] **Step 5: Clone and verify NaX**

```bash
set -euo pipefail
NAX_SHA=bd032d9a0f9b7ce9a72ba336c5d273d019e8bbb3
git clone https://github.com/MaorSabag/NaX "$SPIKE/NaX"
git -C "$SPIKE/NaX" checkout --detach "$NAX_SHA"
actual="$(git -C "$SPIKE/NaX" rev-parse HEAD)"
[ "$actual" = "$NAX_SHA" ]
printf 'nax_sha=%s\n' "$actual" | tee -a "$SPIKE/reports/source-shas.txt"
```

- [ ] **Step 6: Copy the Adaptix source and apply only the disposable build patch**

```bash
set -euo pipefail
mkdir -p "$SPIKE/AdaptixC2"
cp -a "$REPO_ROOT/AdaptixC2/." "$SPIKE/AdaptixC2/"
git apply --verbose "$REPO_ROOT/patches/adaptixserver-go-dependencies.patch" --directory="$SPIKE/AdaptixC2"
```

Expected: the patch applies in the temporary copy only. If it does not apply, classify the result as `Adaptix API or source incompatibility` or `build-layout/dependency drift`, preserve the patch error in `reports/`, and stop this architecture lane.

---

### Task 2: Construct the shared Go workspace and build the Adaptix baseline

**Files:**
- Read: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/AdaptixServer/go.work`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/AdaptixServer/go.work`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/go-work.txt`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/module-graph.txt`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/dist/`

**Interfaces:**
- Consumes: Task 1's disposable source trees and source attestation.
- Produces: a synchronized Go workspace and baseline `adaptixserver` plus seven default extenders for plugin ABI comparison.

- [ ] **Step 1: Add NaX modules to the copied Go workspace**

```bash
set -euo pipefail
ADAPTIX_SERVER="$SPIKE/AdaptixC2/AdaptixServer"
cd "$ADAPTIX_SERVER"
cp go.work "$SPIKE/reports/go-work.baseline"
go work use ./extenders/beacon_agent \
  ./extenders/beacon_listener_dns \
  ./extenders/beacon_listener_http \
  ./extenders/beacon_listener_smb \
  ./extenders/beacon_listener_tcp \
  ./extenders/gopher_agent \
  ./extenders/gopher_listener_tcp \
  "$SPIKE/NaX/src_server/agent_nonameax" \
  "$SPIKE/NaX/src_server/listener_nonameax_http" \
  "$SPIKE/NaX/src_server/listener_nonameax_smb" \
  "$SPIKE/NaX/src_server/service_nax_store"
go work sync
cp go.work "$SPIKE/reports/go.work.txt"
```

Expected: the copied `go.work` contains the AdaptixServer module, seven default extender modules, and the four NaX module paths. The repository's `AdaptixC2/AdaptixServer/go.work` remains untouched.

- [ ] **Step 2: Record the synchronized module graph**

```bash
set -euo pipefail
cd "$ADAPTIX_SERVER"
GOWORK="$ADAPTIX_SERVER/go.work" GOEXPERIMENT=jsonv2,greenteagc go env GOWORK GOVERSION GOEXPERIMENT | tee "$SPIKE/reports/go-env.txt"
GOWORK="$ADAPTIX_SERVER/go.work" GOEXPERIMENT=jsonv2,greenteagc go list -m all | tee "$SPIKE/reports/module-graph.txt"
```

Expected: `GOWORK` points to the disposable workspace; the graph contains `github.com/Adaptix-Framework/axc2 v1.2.0`; no NaX module resolves from an independently installed Go tree.

- [ ] **Step 3: Build AdaptixServer and the existing extenders**

```bash
set -euo pipefail
GOEXPERIMENT=jsonv2,greenteagc make -C "$SPIKE/AdaptixC2" server-ext 2>&1 | tee "$SPIKE/reports/adaptix-server-ext-build.log"
```

Expected: the copied tree produces `dist/adaptixserver`, seven default extender directories, `dist/profile.yaml`, and `dist/ssl_gen.sh`. A failure is recorded as a baseline build blocker before NaX-specific conclusions are made.

- [ ] **Step 4: Verify baseline ELF architecture**

```bash
set -euo pipefail
file "$SPIKE/AdaptixC2/dist/adaptixserver" | tee "$SPIKE/reports/baseline-file.txt"
python3 - "$SPIKE/AdaptixC2/dist/extenders" "$spike_arch" <<'PY'
import pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1])
arch = sys.argv[2]
expected = {"amd64": "x86-64", "arm64": "ARM aarch64"}[arch]
paths = [pathlib.Path(sys.argv[1]).parent.parent / "adaptixserver"] + sorted(root.glob("*/**/*.so"))
for path in paths:
    text = subprocess.check_output(["file", str(path)], text=True).strip()
    print(text)
    if expected not in text:
        raise SystemExit(f"unexpected architecture for {path}: {text}")
PY
```

Expected: amd64 reports `x86-64`; arm64 reports `ARM aarch64`. The baseline build must pass before NaX plugin results are interpreted.

---

### Task 3: Build and stage the four NaX plugins explicitly

**Files:**
- Read: `/tmp/adaptix-nax-spike-<arch>/NaX/src_server/agent_nonameax/`
- Read: `/tmp/adaptix-nax-spike-<arch>/NaX/src_server/listener_nonameax_http/`
- Read: `/tmp/adaptix-nax-spike-<arch>/NaX/src_server/listener_nonameax_smb/`
- Read: `/tmp/adaptix-nax-spike-<arch>/NaX/src_server/service_nax_store/`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/dist/extenders/agent_nonameax/`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/dist/extenders/listener_nonameax_http/`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/dist/extenders/listener_nonameax_smb/`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/dist/extenders/service_nax_store/`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/nax-tests.log`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/nax-build.log`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/asset-inventory.txt`

**Interfaces:**
- Consumes: Task 2's disposable `go.work`, built server, and `dist/extenders/` root.
- Produces: four architecture-matching `.so` files and all NaX runtime assets staged at paths referenced by the temporary profile.

- [ ] **Step 1: Run NaX Go tests in the shared workspace**

```bash
set -euo pipefail
ADAPTIX_SERVER="$SPIKE/AdaptixC2/AdaptixServer"
for module in \
  agent_nonameax \
  listener_nonameax_http \
  listener_nonameax_smb \
  service_nax_store; do
  module_dir="$SPIKE/NaX/src_server/$module"
  printf '\n== %s ==\n' "$module" | tee -a "$SPIKE/reports/nax-tests.log"
  (cd "$module_dir" && GOWORK="$ADAPTIX_SERVER/go.work" GOEXPERIMENT=jsonv2,greenteagc go test ./...) 2>&1 | tee -a "$SPIKE/reports/nax-tests.log"
done
```

Expected: all available tests pass. Any failure is retained with its module name and classified before proceeding; do not silently treat a failing test as a successful compatibility result.

- [ ] **Step 2: Create disposable plugin destinations**

```bash
set -euo pipefail
DIST="$SPIKE/AdaptixC2/dist"
mkdir -p "$DIST/extenders"/{agent_nonameax,listener_nonameax_http,listener_nonameax_smb,service_nax_store}
```

- [ ] **Step 3: Build each plugin with the shared Go workspace**

```bash
set -euo pipefail
ADAPTIX_SERVER="$SPIKE/AdaptixC2/AdaptixServer"
DIST="$SPIKE/AdaptixC2/dist"
export GOWORK="$ADAPTIX_SERVER/go.work"
export GOEXPERIMENT=jsonv2,greenteagc

(
  cd "$SPIKE/NaX/src_server/agent_nonameax"
  go build -buildmode=plugin -o "$DIST/extenders/agent_nonameax/agent_nonameax.so" .
)
(
  cd "$SPIKE/NaX/src_server/listener_nonameax_http"
  go build -buildmode=plugin -o "$DIST/extenders/listener_nonameax_http/listener_nonameax_http.so" .
)
(
  cd "$SPIKE/NaX/src_server/listener_nonameax_smb"
  go build -buildmode=plugin -o "$DIST/extenders/listener_nonameax_smb/listener_nonameax_smb.so" .
)
(
  cd "$SPIKE/NaX/src_server/service_nax_store"
  go build -buildmode=plugin -ldflags='-s -w' -o "$DIST/extenders/service_nax_store/nax_store.so" .
)
```

Expected: four non-empty `.so` files. A failure in one module is classified independently; do not replace it with a prebuilt plugin.

- [ ] **Step 4: Copy only required NaX runtime assets**

```bash
set -euo pipefail
DIST="$SPIKE/AdaptixC2/dist"
NAX="$SPIKE/NaX"
cp "$NAX/src_server/agent_nonameax/config.yaml" "$DIST/extenders/agent_nonameax/config.yaml"
cp "$NAX/src_server/agent_nonameax/ax_config.axs" "$DIST/extenders/agent_nonameax/ax_config.axs"
cp -a "$NAX/src_server/agent_nonameax/pe_templates" "$DIST/extenders/agent_nonameax/pe_templates"
printf '%s\n' "$NAX" > "$DIST/extenders/agent_nonameax/nax_root.conf"
cp "$NAX/src_server/listener_nonameax_http/config.yaml" "$DIST/extenders/listener_nonameax_http/config.yaml"
cp "$NAX/src_server/listener_nonameax_http/ax_config.axs" "$DIST/extenders/listener_nonameax_http/ax_config.axs"
cp "$NAX/src_server/listener_nonameax_smb/config.yaml" "$DIST/extenders/listener_nonameax_smb/config.yaml"
cp "$NAX/src_server/listener_nonameax_smb/ax_config.axs" "$DIST/extenders/listener_nonameax_smb/ax_config.axs"
cp "$NAX/src_server/service_nax_store/config.yaml" "$DIST/extenders/service_nax_store/config.yaml"
cp "$NAX/src_server/service_nax_store/ax_config.axs" "$DIST/extenders/service_nax_store/ax_config.axs"
```

Expected: no NaX source tree is copied into `dist`; only plugin binaries, configuration, AxScript, templates, and the intentional temporary source-root pointer are present.

- [ ] **Step 5: Verify plugin architecture and staged files**

```bash
set -euo pipefail
DIST="$SPIKE/AdaptixC2/dist"
{
  for path in "$DIST/extenders"/*/*.so; do
    file "$path"
    sha256sum "$path"
    go version -m "$path" || true
  done
} | tee "$SPIKE/reports/nax-build.log"
python3 - "$DIST/extenders" "$SPIKE/reports/asset-inventory.txt" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
out = pathlib.Path(sys.argv[2])
required = {
    "agent_nonameax": ["agent_nonameax.so", "config.yaml", "ax_config.axs", "nax_root.conf", "pe_templates"],
    "listener_nonameax_http": ["listener_nonameax_http.so", "config.yaml", "ax_config.axs"],
    "listener_nonameax_smb": ["listener_nonameax_smb.so", "config.yaml", "ax_config.axs"],
    "service_nax_store": ["nax_store.so", "config.yaml", "ax_config.axs"],
}
lines = []
for module, names in required.items():
    base = root / module
    for name in names:
        path = base / name
        if not path.exists():
            raise SystemExit(f"missing staged asset: {path}")
        if path.is_file():
            digest = hashlib.sha256(path.read_bytes()).hexdigest()
            lines.append(f"{module}/{name} sha256={digest} bytes={path.stat().st_size}")
        else:
            files = sorted(p for p in path.rglob("*") if p.is_file())
            lines.append(f"{module}/{name} files={len(files)}")
out.write_text("\n".join(lines) + "\n")
print(out.read_text(), end="")
PY
```

Expected: all four NaX plugins are non-empty and match the native server architecture; every required asset is present and hashed. The `go version -m` output is retained for ABI evidence.

---

### Task 4: Register NaX in a temporary profile and verify server loading

**Files:**
- Read: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/dist/profile.yaml`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/AdaptixC2/dist/profile.nax.yaml`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/server.log`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/server-health.txt`
- Create temporarily: `/tmp/adaptix-nax-spike-<arch>/reports/plugin-load-check.txt`

**Interfaces:**
- Consumes: Task 3's staged NaX plugin tree and Task 2's built server/profile.
- Produces: direct plugin loader evidence and a successful temporary server health check.

- [ ] **Step 1: Create a temporary profile with all NaX entries**

```bash
set -euo pipefail
DIST="$SPIKE/AdaptixC2/dist"
cp "$DIST/profile.yaml" "$DIST/profile.nax.yaml"
python3 - "$DIST/profile.nax.yaml" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
text = path.read_text()
marker = '  extenders:\n'
entries = (
    '    - "extenders/agent_nonameax/config.yaml"\n'
    '    - "extenders/listener_nonameax_http/config.yaml"\n'
    '    - "extenders/listener_nonameax_smb/config.yaml"\n'
    '    - "extenders/service_nax_store/config.yaml"\n'
)
if marker not in text:
    raise SystemExit('profile extenders block not found')
if 'extenders/agent_nonameax/config.yaml' in text:
    raise SystemExit('NaX entries already present; refuse ambiguous profile mutation')
path.write_text(text.replace(marker, marker + entries, 1))
PY
```

Expected: the temporary profile contains the seven baseline extender entries plus exactly four NaX entries. The committed profile remains unchanged.

- [ ] **Step 2: Generate temporary TLS material**

```bash
set -euo pipefail
cd "$SPIKE/AdaptixC2/dist"
./ssl_gen.sh
[ -s server.rsa.crt ]
[ -s server.rsa.key ]
```

Expected: the copied server can start without writing into the repository.

- [ ] **Step 3: Start the temporary server and capture its logs**

```bash
set -euo pipefail
DIST="$SPIKE/AdaptixC2/dist"
(
  cd "$DIST"
  exec ./adaptixserver -profile "$DIST/profile.nax.yaml"
) > "$SPIKE/reports/server.log" 2>&1 &
server_pid=$!
printf '%s\n' "$server_pid" > "$SPIKE/reports/server.pid"
cleanup() { kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; }
trap cleanup EXIT
```

- [ ] **Step 4: Wait for the HTTPS endpoint and record the result**

```bash
set -euo pipefail
for attempt in $(seq 1 30); do
  status="$(curl -ksS --max-time 2 -o /dev/null -w '%{http_code}' https://127.0.0.1:4321/endpoint || true)"
  if [ "$status" != "000" ]; then
    printf 'http_status=%s\n' "$status" | tee "$SPIKE/reports/server-health.txt"
    break
  fi
  sleep 1
done
[ -s "$SPIKE/reports/server-health.txt" ]
```

Expected: a non-`000` HTTPS response. The endpoint is a WebSocket route, so a normal HTTP status is sufficient to prove the server reached its HTTP/TLS layer; it is not expected to return a normal application `2xx` response to plain GET.

- [ ] **Step 5: Check loader errors and NaX registration evidence**

```bash
set -euo pipefail
python3 - "$SPIKE/reports/server.log" <<'PY' | tee "$SPIKE/reports/plugin-load-check.txt"
from pathlib import Path
import sys
text = Path(sys.argv[1]).read_text(errors="replace")
error_markers = (
    "failed to open plugin",
    "failed to find InitPlugin",
    "unexpected signature",
    "returned nil",
    "does not registered",
    "Config extenders/",
    "Error config",
)
for marker in error_markers:
    matches = [line for line in text.splitlines() if marker in line]
    print(f"{marker}: {len(matches)}")
    for line in matches:
        print(f"  {line}")
if any(marker in text for marker in error_markers):
    raise SystemExit("plugin loader errors detected")
for name in ("agent_nonameax", "listener_nonameax_http", "listener_nonameax_smb", "service_nax_store"):
    print(f"staged module present: {name}")
PY
```

Expected: zero loader-error markers and a running healthy server. For `service_nax_store`, also retain the explicit `Service '<name>' loaded` success line if emitted. If the server logs do not expose a positive registration line for agent/listener modules, record the absence as a limitation and use the absence of loader errors plus successful server initialization as the load evidence; do not claim payload generation succeeded.

---

### Task 5: Repeat natively on arm64 and produce the blocker/pass report

**Files:**
- Create temporarily: `/tmp/adaptix-nax-spike-arm64/`
- Create temporarily: `/tmp/adaptix-nax-spike-arm64/reports/final-report.md`
- Create temporarily: `/tmp/adaptix-nax-spike-arm64/reports/objcopy-check.txt`
- Read: `/tmp/adaptix-nax-spike-amd64/reports/`

**Interfaces:**
- Consumes: the completed amd64 evidence and the same reviewed source SHAs.
- Produces: native arm64 evidence and the final compatibility decision.

- [ ] **Step 1: Repeat Tasks 1–4 on a native arm64 host**

Use the exact commands and source SHAs from Tasks 1–4 with `spike_arch=arm64`. Do not reuse amd64 `.so` files, Go build caches, native build directories, or staged artifacts. The arm64 workspace must be independently created under `/tmp/adaptix-nax-spike-arm64/`.

- [ ] **Step 2: Prove cross-target `objcopy` selection**

```bash
set -euo pipefail
command -v x86_64-w64-mingw32-objcopy | tee "$SPIKE/reports/objcopy-check.txt"
x86_64-w64-mingw32-objcopy --version | tee -a "$SPIKE/reports/objcopy-check.txt"
python3 - "$SPIKE/NaX" <<'PY'
from pathlib import Path
import sys
root = Path(sys.argv[1])
text = "\n".join(p.read_text(errors="replace") for p in root.rglob("Makefile"))
if "objcopy" not in text:
    raise SystemExit("NaX Makefiles contain no objcopy reference to validate")
print("NaX Makefiles reference objcopy; explicit MinGW objcopy was provisioned")
PY
```

Expected: the MinGW cross-target tool is present and the arm64 report records it. If the source invokes bare `objcopy` in an artifact path, classify the result as a native cross-architecture blocker and do not mark arm64 passed until an explicit source/build override is tested.

- [ ] **Step 3: Compare architecture and module evidence**

Confirm the amd64 report contains only x86-64 server/plugins and the arm64 report contains only AArch64 server/plugins. Compare the two `go.work` files, module graphs, source SHAs, and toolchain reports; only host architecture and native tool paths may differ.

- [ ] **Step 4: Write the final report**

Create `reports/final-report.md` with these exact sections:

```markdown
# NaX Compatibility Spike Report

## Inputs

## Host and toolchain

## Go workspace and ABI evidence

## NaX plugin build results

## Runtime asset inventory

## Temporary server load and health results

## Native arm64 cross-tool result

## Blockers

## Decision
```

Under `Decision`, use exactly one of:

- `PASS: NaX plugins compile and load on native amd64 and arm64; proceed to a separate experimental-integration design.`
- `BLOCKED: NaX compatibility is not proven; see the classified blocker and reproduction evidence.`

Do not call a result production-ready. A pass authorizes only the next design phase; it does not authorize adding NaX to the default image.

- [ ] **Step 5: Remove disposable workspaces after evidence is preserved**

After the final report and all logs are copied to the agreed evidence location, remove `/tmp/adaptix-nax-spike-amd64` and `/tmp/adaptix-nax-spike-arm64`. Verify that no NaX files, generated binaries, profile entries, or submodule edits remain in the repository.

---

## Self-review checklist

- [ ] Every requirement in `docs/superpowers/specs/2026-08-02-nax-compatibility-spike-design.md` maps to at least one task above.
- [ ] The plan never modifies the supported runtime or committed NaX integration files.
- [ ] The four NaX modules are built explicitly in the shared Go workspace.
- [ ] The NaX Makefiles' incompatible deployment paths are bypassed.
- [ ] Both native architectures are required, not inferred from QEMU output.
- [ ] The arm64 lane checks x86-64 Windows tooling explicitly.
- [ ] Payload generation is not accidentally treated as proven by plugin loading.
- [ ] All evidence and blocker classifications have concrete output paths.
- [ ] No incomplete placeholder or unspecified implementation step remains.
