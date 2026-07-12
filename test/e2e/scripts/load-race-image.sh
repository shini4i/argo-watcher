#!/usr/bin/env bash
# Load a locally-built image into a kind node's containerd.
#
# `kind load docker-image` is broken with the podman provider + containerd 2.x:
# kind invokes `ctr images import --all-platforms`, which fails on a
# single-arch image with "no unpack platforms defined". We replicate kind's
# import minus that flag, pinning the platform explicitly instead.
#
# Usage: load-race-image.sh <image[:tag]> [cluster-name]
set -euo pipefail

IMAGE="${1:?usage: load-race-image.sh <image[:tag]> [cluster-name]}"
CLUSTER="${2:-aw-e2e}"
NODE="${CLUSTER}-control-plane"
PLATFORM="${PLATFORM:-linux/amd64}"

tmp="$(mktemp --suffix=.tar)"
trap 'rm -f "$tmp"' EXIT

docker save "$IMAGE" -o "$tmp"
podman exec -i "$NODE" \
  ctr --namespace=k8s.io images import --platform "$PLATFORM" --digests - < "$tmp"

echo "loaded $IMAGE into $NODE ($PLATFORM)"
