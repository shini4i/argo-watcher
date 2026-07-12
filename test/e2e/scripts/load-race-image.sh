#!/usr/bin/env bash
# Load a locally-built image into a kind node's containerd, on either the docker
# or the podman kind provider.
#
#   - docker provider (e.g. GitHub-hosted runners): `kind load docker-image`
#     works directly.
#   - podman provider (local dev): `kind load` is broken — kind passes
#     `--all-platforms` to `ctr import`, which fails on a single-arch image with
#     "no unpack platforms defined". We fall back to replicating kind's import
#     minus that flag, pinning the platform explicitly.
#
# Usage: load-race-image.sh <image[:tag]> [cluster-name]
set -euo pipefail

IMAGE="${1:?usage: load-race-image.sh <image[:tag]> [cluster-name]}"
CLUSTER="${2:-aw-e2e}"

# Fast path (docker provider).
if kind load docker-image "$IMAGE" --name "$CLUSTER" 2>/dev/null; then
  echo "loaded $IMAGE via 'kind load docker-image'"
  exit 0
fi

# Fallback (podman provider): import straight into the node's containerd.
echo "'kind load' failed (podman provider?); importing via ctr" >&2
NODE="${CLUSTER}-control-plane"
PLATFORM="${PLATFORM:-linux/amd64}"
tmp="$(mktemp --suffix=.tar)"
trap 'rm -f "$tmp"' EXIT

docker save "$IMAGE" -o "$tmp"
podman exec -i "$NODE" \
  ctr --namespace=k8s.io images import --platform "$PLATFORM" --digests - < "$tmp"
echo "loaded $IMAGE into $NODE ($PLATFORM) via ctr import"
