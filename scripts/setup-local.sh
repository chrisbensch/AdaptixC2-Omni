#!/usr/bin/env bash
#
# Local-only dev setup for the Nax sidecar workspace.
#
# The AdaptixC2 submodule's go.work needs two extra "use" entries (the pinned
# NaX agent module + the sidecar) so that `go vet`/`go test`/`go build` resolve
# the agent->sidecar local `replace`. These entries are LAYOUT-SPECIFIC:
# locally they are ../../NaX/... and ../../sidecar/..., but the Docker build
# strips them and re-adds container-correct paths. This is therefore NOT
# committed inside the submodule (AGENTS.md) — it is applied here from a
# version-controlled patch, so any checkout can reproduce the local workspace.
#
# Idempotent: safe to run repeatedly (skips if already wired). Callable from any
# cwd; resolves paths via $REPO_ROOT.
#
# Usage: ./scripts/setup-local.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

SUBMODULE="AdaptixC2"
GO_WORK="${REPO_ROOT}/${SUBMODULE}/AdaptixServer/go.work"
PATCH="${REPO_ROOT}/patches/adaptixc2-go-work.patch"

# Marker line the patch adds; used for idempotency.
MARKER="// Nax agent module (submodule)"

if [ ! -f "${GO_WORK}" ]; then
	echo "[setup-local] missing ${GO_WORK} — is the ${SUBMODULE} submodule checked out?" >&2
	exit 1
fi

if grep -qF "${MARKER}" "${GO_WORK}"; then
	echo "[setup-local] already wired — skipping (${SUBMODULE}/AdaptixServer/go.work)"
	exit 0
fi

if [ ! -f "${PATCH}" ]; then
	echo "[setup-local] missing patch ${PATCH}" >&2
	exit 1
fi

# Apply from inside the submodule so the patch's a/AdaptixServer/go.work path
# resolves with -p1 (the paths are submodule-relative, not repo-root-relative).
( cd "${REPO_ROOT}/${SUBMODULE}" && patch -p1 --forward < "${PATCH}" )

echo "[setup-local] wired ${SUBMODULE}/AdaptixServer/go.work"
