# argo-watcher end-to-end lab

A disposable, reproducible lab that runs **real** ArgoCD, Gitea, and
argo-rollouts (the latter only for the `accept-suspended` canary-pause phase) on
a single-node [kind](https://kind.sigs.k8s.io/) cluster and deploys argo-watcher
built with the Go **race detector**. It exercises the code paths the fast test
suites cannot: the real ArgoCD polling loop, sustained-concurrency data races,
and the real git push path — once per release, not on every PR. The `smoke`,
`failure-diagnostics`, and `race` phases drive the **real `cmd/client` binary**
(not a hand-rolled HTTP call), so the tool users actually run is covered
end-to-end: success (exit 0), a surfaced failure reason (exit 1), and the
superseded/cancelled path (exit 1). The `load` soak stays a purpose-built
concurrent driver (`load/`) — it asserts server behaviour under contention, not
client behaviour.

Unlike the unit and integration suites, ArgoCD here is **not mocked**.

## Prerequisites

`kind`, `kubectl`, `helm`, `go`, `git`, `jq`, `curl`, `task`, and **podman** as
the kind provider (`KIND_EXPERIMENTAL_PROVIDER=podman`). The image-load step uses
`podman exec` against the kind node, so a podman-backed cluster is required; a
`docker` CLI that is a podman shim works, a real docker-provider cluster does
not. `go` builds the client binary and runs the load driver, and `git` drives
the competitor writer. All pinned tool/chart versions are in `Taskfile.yml`.

## Usage

```sh
task e2e     # one-shot per-release run: up → api-surface → smoke → client-knobs → jwt-auth → fire-and-forget → commit-format → multi-image → accept-suspended → docker-proxy → lockdown → notifications → load → batch-writeback → race → state-postgres → failure-diagnostics → argocd-unreachable → shutdown-drain → down
```

`task e2e` walks the whole flow. It stops on the first failing step, so a failed
run leaves the cluster up for debugging; a fully green run tears it down.

Individual steps (for iterating or debugging):

```sh
task up                   # build the race image + boot the full lab (idempotent)
task verify               # assert argo-watcher is up and reaching real Argo
task api-surface          # assert the read-only HTTP surface (version/config/task-list/deploy-lock) to contract
task smoke                # one authenticated deploy through the full write-back loop, via the real client binary
task client-knobs         # assert client env knobs: TASK_REFRESH override deploys, DEBUG cURL log redacts the token
task jwt-auth             # assert the JWT (BEARER_TOKEN) auth path drives an authenticated write-back to deployed
task fire-and-forget      # assert fire-and-forget mode: a managed CronJob app's write-back reaches deployed without the image rolling out
task commit-format        # assert COMMIT_MESSAGE_FORMAT renders into the git write-back commit message
task multi-image          # assert a multi-image deploy bumps and writes back both images in one commit
task accept-suspended     # assert ACCEPT_SUSPENDED_APP treats a paused argo-rollouts Rollout (Suspended) as deployed
task docker-proxy         # assert DOCKER_IMAGES_PROXY matches a bare image name against the proxy-prefixed running image
task lockdown             # assert LOCKDOWN_SCHEDULE freezes deploys in-window (406) and the watcher broadcasts "locked"
task notifications        # assert the generic webhook fires (start + result) with the correct payload
task load                 # git-conflict soak: competitor + concurrent deploys, strict 0-failed
task batch-writeback      # toggle GIT_BATCH_WRITEBACK on, re-run the contention soak: assert 0 lost updates + real coalescing (mean batch size > 1), then revert
task race                 # same-app supersession: a newer deploy must win over an older retrying one
task state-postgres       # flip the release to Postgres state: assert migration, deploy loop, task survives a pod restart, supersession under contention
task failure-diagnostics  # assert failure reasons carry the real cause (pod ImagePullBackOff, failed hooks)
task argocd-unreachable   # scale ArgoCD down: assert /argocd-status flips + the watcher broadcasts "argocd_down" + POST fast-fails 503, then recovers
task shutdown-drain       # assert graceful shutdown drains in-flight WebSocket connections (GoingAway) with no race/panic
task down                 # destroy the cluster
```

Tunable soak knobs are `Taskfile.yml` vars (`APPS`, `WORKERS`, `WS_CLIENTS`,
`SOAK`, `SOAK_SECONDS`, `COMPETITOR_INTERVAL`, plus `BATCH_SOAK` / `BATCH_SOAK_SECONDS`
for the batch-writeback phase), overridable on the CLI, e.g.
`task e2e SOAK=10m WORKERS=20`.

## CI

The same flow runs in GitHub Actions via the **E2E lab** workflow: add the
**`e2e`** label to a pull request and it runs the full flow against that PR's
branch (re-run by removing and re-adding the label). It is also dispatchable
manually (`workflow_dispatch`) once the workflow is on the default branch. Runs
on a hosted `ubuntu-latest` runner, where kind uses the **docker** provider —
`load-race-image.sh` takes its `kind load` fast path there and falls back to the
podman `ctr import` locally, so the lab runs unchanged in both places.

Reach any component with `kubectl port-forward` (there is no ingress), e.g.
`kubectl -n argo-watcher port-forward svc/argo-watcher 8080:80`.

## Layout

| Path | Purpose |
|---|---|
| `Dockerfile.server.race` | argo-watcher built with `-race` on a glibc distroless base |
| `kind-config.yaml` | single-node cluster |
| `values/` | pinned Helm values for argocd / argo-watcher / gitea / webhook-tester |
| `scripts/load-race-image.sh` | load a local image into the kind node |
| `scripts/mint-argo-token.sh` | mint `ARGO_TOKEN` into `argo-watcher-secret` |
| `scripts/failure-diagnostics.sh` | table-driven failure-reason assertions, driven through the real client (add a case = one table entry) |
| `scripts/race-supersede.sh` | same-app supersession assertion: real client, newer deploy wins, older is superseded |
| `scripts/state-postgres.sh` | flip the release to `STATE_TYPE=postgres` and assert the Postgres-only path: schema migration Job, real deploy loop, task history surviving a pod restart (in-memory loses it), and supersession under contention (the hand-written `CancelInProgressTasks` SQL) |
| `scripts/batch-writeback.sh` | toggle `GIT_BATCH_WRITEBACK=true` on the release, re-run the contention soak, and revert; reuses `collect.sh` (with `BATCH_MODE`) to gate on zero lost updates and real coalescing (`gitops_batch_size` mean > 1) |
| `fixtures/postgres/` | in-cluster Postgres (Secret + Service + StatefulSet, one resource per file) the `state-postgres` phase points the release at; the chart bundles no database |
| `values/argo-watcher-postgres.yaml` | overlay layered over `values/argo-watcher.yaml` that enables `postgres` (sets `STATE_TYPE=postgres`, wires `DB_*`, triggers the migration Job) |
| `scripts/hook-fixture.sh` | add/remove a failing PreSync hook via the chart's `rawObject` |
| `scripts/notifications.sh` | assert the generic webhook fires start + result with the templated payload and auth header |
| `scripts/api-surface.sh` | assert the read-only HTTP surface to contract: version/config (secrets redacted), task-list filters + invalid-status 400, unknown-task 404, deploy-lock POST/DELETE 404 when Keycloak is off |
| `scripts/client-knobs.sh` | assert client env knobs via the real client: `TASK_REFRESH=false` still deploys, `DEBUG=true` cURL log redacts the deploy token |
| `scripts/jwt-auth.sh` | assert the JWT (`BEARER_TOKEN`) auth path: mint an HS256 token, deploy with no deploy token, prove the authenticated write-back reaches deployed |
| `tools/mintjwt/` | tiny Go HS256 JWT minter (signs with the server's own jwt library; avoids an openssl dependency) |
| `scripts/fire-and-forget.sh` | assert `argo-watcher/fire-and-forget` on a managed CronJob app: the write-back updates the CronJob's image and the deploy reports "deployed" even though the image never rolls out (no pod until the schedule fires) |
| `fixtures/fire-and-forget-app.yaml` + `fixtures/fire-and-forget-chart/` | dedicated `ffapp` Argo Application (managed) and its CronJob chart (image tag a write-back target, effectively-never schedule), outside the app1..N soak range |
| `scripts/commit-format.sh` | assert `COMMIT_MESSAGE_FORMAT` renders into the real write-back commit message (reads the commit back from the gitops repo) |
| `scripts/multi-image.sh` | assert a two-image deploy reaches "deployed" and writes back both image-tag overrides in one commit |
| `fixtures/multi-image/` | two-image umbrella: the `app` chart (primary image) plus a second image via the chart's rawObject passthrough |
| `fixtures/multi-image-app.yaml` | dedicated `multiapp` Argo Application declaring two managed images mapped to two Helm image-tag values |
| `scripts/accept-suspended.sh` | assert `ACCEPT_SUSPENDED_APP` accepts a paused Rollout as deployed (write-back triggers a canary pause) |
| `fixtures/rollout-chart/` + `fixtures/suspended-app.yaml` | `suspendapp`: a managed argo-rollouts Rollout (canary pause step); the write-back bump pauses it mid-rollout so ArgoCD reports it Suspended |
| `scripts/docker-proxy.sh` | assert `DOCKER_IMAGES_PROXY` matches a bare image against the proxy-prefixed running image |
| `fixtures/proxy-app.yaml` | `proxyapp`: reuses the shared chart with the image repository overridden to `mirror.gcr.io/traefik/whoami` |
| `scripts/lockdown.sh` | assert scheduled lockdown: toggles `LOCKDOWN_SCHEDULE` on the release (window opening ~3 min out) and reverts, asserting in-window deploys are rejected (406), `GET /deploy-lock` reports `true`, and the watcher broadcasts `"locked"` on the transition |
| `scripts/shutdown-drain.sh` | assert graceful shutdown: hold WebSocket clients open, delete the pod, and assert every client sees a `1001 "server shutdown"` close and the logs show the ordered drain with no data race / panic / drain timeout |
| `scripts/argocd-unreachable.sh` | assert the ArgoCD-unreachable signal (#498): scale `argocd-server` to 0, assert `GET /argocd-status` flips to `false`, the watcher broadcasts `"argocd_down"`, and `POST /tasks` fast-fails `503 {"status":"down"}` (well under the retry budget); then scale back up and assert recovery (`true`, `"argocd_up"`, `202`) |
| `tools/wsprobe/` | tiny Go WebSocket probe used by `lockdown` (grep for the `"locked"` broadcast), `shutdown-drain` (assert the graceful GoingAway close), and `argocd-unreachable` (grep for `"argocd_down"`/`"argocd_up"`), streaming `MSG`/`CLOSED` events one per line |

## Gotchas (why the scripts exist)

- **`kind load docker-image` is broken with podman + containerd 2.x** — kind
  passes `--all-platforms` to `ctr import`, which fails on a single-arch image
  ("no unpack platforms defined"). `load-race-image.sh` imports via `ctr` with
  an explicit `--platform` instead.
- **The `-race` image needs a glibc base.** `go build -race` forces
  `CGO_ENABLED=1` and dynamic linking, so `Dockerfile.server.race` uses
  `gcr.io/distroless/base-debian12` — the production `distroless/static` base
  cannot run it.
- **Webhook notifications are enabled globally, not just for `notifications`.**
  The webhook is env-configured at install (`values/argo-watcher.yaml`
  `extraEnvs`) pointing at the in-cluster `webhook-tester` receiver, so *every*
  task fires start + result webhooks — the soak/race phases exercise the
  notifier under `-race` for free. `notifications.sh` still gets a deterministic
  assertion by running on a clean state and filtering the receiver's capture on
  its own task id. The receiver is the generic `app` chart running
  `tarampampam/webhook-tester` (in-memory, single container, no DB/Redis); its
  `AUTO_CREATE_SESSIONS` makes the fixed-UUID `WEBHOOK_URL` work with no startup
  wiring (the `WEBHOOK_UUID` in `Taskfile.yml` and the URL must match).

- **`LOCKDOWN_SCHEDULE` is toggled in place, not set globally.** It is a
  server-global freeze, so enabling it in the shared install would block every
  other deploy phase. `lockdown.sh` instead helm-upgrades the live release with a
  schedule whose window *opens ~3 minutes in the future* and reverts before it
  returns. The future start is deliberate: the pod boots unlocked and then
  crosses into the window while a `wsprobe` client watches, which is the only way
  to observe a *scheduled* `"locked"` broadcast (the watcher notifies on state
  change, not at boot, and polls at minute granularity). The 406 + `GET`-true +
  revert-accepts checks are deterministic; the WS-transition sub-check is skipped
  (not failed) if a slow rollout boots in-window. Manual (Keycloak) lock/unlock
  WS notifications are a separate, deterministic trigger left to the heavy tier.
- **`state-postgres` flips the shared release to Postgres mid-flow rather than
  running a parallel stack.** The base lab is single-replica in-memory; every
  functional phase before this one validates that backend. `state-postgres` then
  helm-upgrades the SAME release with `values/argo-watcher-postgres.yaml` (the
  chart bundles no database, so `fixtures/postgres/` supplies an in-cluster
  Postgres first) and asserts the Postgres-only properties — the migration Job,
  the deploy loop, a task record surviving a pod restart, and supersession under
  contention. It runs *before* `failure-diagnostics` so it deploys against
  pristine apps; the two phases after it are backend-agnostic, so they simply run
  on Postgres for free. Multi-replica is out of scope — the chart requires
  postgres for `replicaCount > 1`, and shared-state / cross-replica handoff is
  deliberately not exercised.
- **`shutdown-drain` follows the pod's logs before deleting it.** A recreated
  StatefulSet pod is a new object, so `kubectl logs --previous` would not have the
  terminated instance's shutdown logs; the script streams `logs -f` to a file
  first, then deletes the pod with `--grace-period=60` so the full drain
  (`srv.Shutdown` 30s + WS drain 10s) completes before SIGKILL.
- **Several config toggles are set globally via `values/argo-watcher.yaml`
  `extraEnvs`** so a single boot can assert them: `JWT_SECRET` (jwt-auth),
  `COMMIT_MESSAGE_FORMAT` (commit-format), `ACCEPT_SUSPENDED_APP` +
  `DOCKER_IMAGES_PROXY` (their same-named phases), and `ARGO_URL_ALIAS` — the
  last makes the client print an externally-shaped ArgoCD link on failure, which
  `failure-diagnostics.sh` asserts. All are harmless to the other phases.

## Topology note

The base lab runs argo-watcher single-replica with in-memory state. The
`state-postgres` phase (see Usage) flips the same release to Postgres, still
single-replica, to exercise the Postgres-backed path. Multi-replica
(`replicaCount: 2` + shared state / cross-replica poller handoff) is out of
scope for this lab and remains future work.
