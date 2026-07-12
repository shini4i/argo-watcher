# argo-watcher end-to-end lab

A disposable, reproducible lab that runs **real** ArgoCD and Gitea on a
single-node [kind](https://kind.sigs.k8s.io/) cluster and deploys argo-watcher
built with the Go **race detector**. It exercises the code paths the fast test
suites cannot: the real ArgoCD polling loop, sustained-concurrency data races,
and the real git push path — once per release, not on every PR.

Unlike the unit and integration suites, ArgoCD here is **not mocked**.

## Prerequisites

`kind`, `kubectl`, `helm`, `jq`, `curl`, `task`, and **podman** as the kind
provider (`KIND_EXPERIMENTAL_PROVIDER=podman`). The image-load step uses
`podman exec` against the kind node, so a podman-backed cluster is required; a
`docker` CLI that is a podman shim works, a real docker-provider cluster does
not. All pinned tool/chart versions are in `Taskfile.yml`.

## Usage

```sh
task up      # build the race image + boot the full lab (idempotent)
task verify  # assert argo-watcher is up and reaching real Argo
task smoke   # one authenticated deploy through the full write-back loop
task down    # destroy the cluster
```

Reach any component with `kubectl port-forward` (there is no ingress), e.g.
`kubectl -n argo-watcher port-forward svc/argo-watcher 8080:80`.

## Layout

| Path | Purpose |
|---|---|
| `Dockerfile.server.race` | argo-watcher built with `-race` on a glibc distroless base |
| `kind-config.yaml` | single-node cluster |
| `values/` | pinned Helm values for argocd / argo-watcher / gitea |
| `scripts/load-race-image.sh` | load a local image into the kind node |
| `scripts/mint-argo-token.sh` | mint `ARGO_TOKEN` into `argo-watcher-secret` |

## Gotchas (why the scripts exist)

- **`kind load docker-image` is broken with podman + containerd 2.x** — kind
  passes `--all-platforms` to `ctr import`, which fails on a single-arch image
  ("no unpack platforms defined"). `load-race-image.sh` imports via `ctr` with
  an explicit `--platform` instead.
- **The `-race` image needs a glibc base.** `go build -race` forces
  `CGO_ENABLED=1` and dynamic linking, so `Dockerfile.server.race` uses
  `gcr.io/distroless/base-debian12` — the production `distroless/static` base
  cannot run it.

## Topology note

The base lab runs argo-watcher single-replica with in-memory state. The soak
phase (see the project plan) flips `values/argo-watcher.yaml` to
`replicaCount: 2` + Postgres to exercise shared state and cross-replica poller
handoff.
