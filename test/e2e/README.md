# argo-watcher end-to-end lab

A disposable lab that runs **real** ArgoCD, Gitea, Keycloak (the OIDC provider the
`app-tokens` phase authenticates against) and argo-rollouts (the last only for the
`accept-suspended` phase) on a single-node
[kind](https://kind.sigs.k8s.io/) cluster, with argo-watcher built under the Go
**race detector**. Nothing is mocked here, which is the point: it covers the real
ArgoCD polling loop, the real git push path, and data races under sustained
concurrency — once per release, not on every PR.

The `smoke`, `failure-diagnostics` and `race` phases drive the **real
`cmd/client` binary**, so the tool users actually run is covered end to end:
success (exit 0), a surfaced failure reason (exit 1), and the superseded path
(exit 1). The `load` soak keeps its purpose-built concurrent driver (`load/`) —
it asserts server behaviour under contention, not client behaviour.

Browser-level UI testing lives separately in `web/e2e/` (Playwright,
`task test-web-e2e`).

## Prerequisites

`kind`, `kubectl`, `helm`, `go`, `git`, `jq`, `yq`, `curl`, `task`, `bats`, and
**podman** as the kind provider (`KIND_EXPERIMENTAL_PROVIDER=podman`). The
image-load step uses `podman exec` against the kind node, so a podman-backed
cluster is required; a `docker` CLI that is a podman shim works, a real
docker-provider cluster does not. `go` builds the client binary and runs the load
driver, and `git` drives the competitor writer. Pinned tool/chart versions are in
`Taskfile.yml`.

`nix develop` provides the lint tooling (`shellcheck`, `bats`, `yq`) — enough for
`task lint`. The cluster tools (`kind`, `kubectl`, `helm`, `jq`, `task`) are not in
the dev shell and must be on `PATH` to run the lab itself.

The lab creates and deletes a kind cluster, which writes to your **kubeconfig**.
Point `KUBECONFIG` at a throwaway file first if your default one holds contexts you
care about.

## Usage

```sh
task e2e     # one-shot per-release run: boot the lab, run every phase, tear it down
```

`task e2e` is `up` → `phases` → `down`. The phases run under **bats**
(`phases.bats`, one test per phase, order documented there). Once a phase fails the
remaining ones are skipped and `down` never runs, so the cluster is left up for
debugging; a fully green run tears it down. A per-phase JUnit report lands in
`reports/`.

```sh
task phases                    # re-run every phase against a lab that is already up
bats --filter lockdown phases.bats          # one phase by name
bats --filter-status failed phases.bats     # only what failed last run
task lint                      # shellcheck + the offline bats suites (no cluster)
```

Individual steps (for iterating or debugging):

```sh
task up                   # build the race image + boot the full lab (idempotent)
task verify               # assert argo-watcher is up and reaching real Argo
task api-surface          # assert the read-only HTTP surface (version/config/task-list/deploy-lock) to contract
task read-auth            # assert OIDC read protection: 401 without a credential, 503 when the provider is unreachable, exemptions still open
task smoke                # one authenticated deploy through the full write-back loop, via the real client binary
task client-knobs         # assert client env knobs: TASK_REFRESH override deploys, DEBUG cURL log redacts the token
task jwt-auth             # assert the JWT (BEARER_TOKEN) auth path drives an authenticated write-back, and that JWT_ISSUER/JWT_AUDIENCE bind the token
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
task state-postgres       # flip the release to Postgres state: assert migration, deploy loop, task survives a pod restart, the shared deploy lock, supersession under contention
task app-tokens           # application deploy tokens end to end: issue through the Keycloak-authenticated API, enforce the per-application scope on a real deploy, revoke, expire
task failure-diagnostics  # assert failure reasons carry the real cause (pod ImagePullBackOff, failed hooks, a degraded migration Job, a stuck rollout)
task argocd-unreachable   # scale ArgoCD down: assert /reachability flips (reason "argocd") + the watcher broadcasts "argocd_down:argocd" + POST fast-fails 503, then recovers
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

## Reaching the lab

Five services are published on fixed **localhost** ports (no ingress, no
port-forward): argo-watcher `30080`, webhook-tester `30081`, Gitea `30300`, ArgoCD
`30443`, Keycloak `30500`. `fixtures/nodeports/` assigns the node ports and
`kind-config.yaml` maps them to the host; kind requires the two to use the same
number, which `ports.bats` asserts. The URLs are exported by `scripts/lib.sh` (`AW_URL`,
`GITEA_URL`, …), so a phase that restarts the server just waits for it to answer
again instead of re-establishing a forward.

## Layout

| Path | Purpose |
|---|---|
| `Dockerfile.server.race` | argo-watcher built with `-race` on a glibc distroless base |
| `kind-config.yaml` | single-node cluster + the host port mappings for the NodePorts below |
| `fixtures/nodeports/` | fixed NodePort Services for the five components phases talk to from the host (one per file, matching `fixtures/postgres/`) |
| `phases.bats` | the per-release phase suite: one test per phase, with the ordering constraints |
| `ports.bats` | offline assertions on the kind-config ↔ nodeports port coupling |
| `scripts/lib.sh` | shared phase helpers: endpoint URLs, `retry`/`wait_*`, `req`, `run_client`, `metric_sum`/`metric_label_sum`, `helm_apply_aw`, `ok`/`bad`/`phase_end` |
| `scripts/lib.bats` | unit tests for lib.sh's pure text-processing helpers (no cluster needed) |
| `scripts/soak.sh` | the git-conflict soak: competitor + concurrent deploys, then the `collect.sh` gates |
| `scripts/verify.sh` | post-`up` gate: readyz 200 and `argocd_unavailable` 0 |
| `values/` | pinned Helm values for argocd / argo-watcher / gitea / webhook-tester |
| `scripts/load-race-image.sh` | load a local image into the kind node |
| `scripts/mint-argo-token.sh` | mint `ARGO_TOKEN` into `argo-watcher-secret` |
| `scripts/failure-diagnostics.sh` | table-driven failure-reason assertions, driven through the real client (add a case = one table entry) |
| `scripts/race-supersede.sh` | same-app supersession assertion: real client, newer deploy wins, older is superseded |
| `scripts/app-tokens.sh` | assert application deploy tokens end to end against the lab's real Keycloak: the endpoints are unregistered and a token refused by name while the feature is off; issuing is restricted to a privileged operator (an unprivileged user and a token minted for another client of the realm are both 401); an issued token records its scope, hint and creator and returns its secret exactly once, with `Cache-Control: no-store`; a scope matching nothing is 406 and one with duplicates/whitespace is normalized; the per-application scope authorizes a REAL deploy of `app4` (write-back committed) while refusing `app2` and near-miss names; a multi-application token covers each of its apps and an all-applications one covers any; `last_used_at` moves on a deployment but never on a read; revocation takes effect on the next request and keeps the audit row; an expired token is refused; the token opens a `/ws` handshake and a CI JWT still works on the same header; and the token survives a pod restart |
| `values/keycloak.yaml` | the in-cluster OIDC provider (generic `app` chart) serving the SAME realm the docker-compose `integration` profile imports, from `test/keycloak/argo-watcher-e2e-realm.json` mounted as a ConfigMap. `KC_HOSTNAME` pins the issuer to the in-cluster Service URL so a token minted from the host over the NodePort is still accepted at the userinfo endpoint the server calls from inside the cluster |
| `scripts/state-postgres.sh` | flip the release to `STATE_TYPE=postgres` and assert the Postgres-only path: schema migration Job, real deploy loop, task history surviving a pod restart (in-memory loses it), the shared deploy lock (a lock row written by another writer rejects deploys here), and supersession under contention (the hand-written `CancelInProgressTasks` SQL) |
| `scripts/batch-writeback.sh` | toggle `GIT_BATCH_WRITEBACK=true` on the release, re-run the contention soak, and revert; reuses `collect.sh` (with `BATCH_MODE`) to gate on zero lost updates and real coalescing (`gitops_batch_size` mean > 1) |
| `fixtures/postgres/` | in-cluster Postgres (Secret + Service + StatefulSet, one resource per file) the `state-postgres` phase points the release at; the chart bundles no database |
| `values/argo-watcher-postgres.yaml` | overlay layered over `values/argo-watcher.yaml` that enables `postgres` (sets `STATE_TYPE=postgres`, wires `DB_*`, triggers the migration Job) |
| `scripts/failure-fixture.sh` | inject/remove a deliberately-broken resource via the chart's `rawObject`: a failing PreSync hook (`hook`, aborts the sync), a failing plain migration Job (`degraded`, lets the image roll out and holds the app Synced+Degraded), or a Deployment whose readiness probe never passes (`pending`, holds the app Synced+Progressing with nothing Degraded); sole owner of the shared `chart/values.yaml` |
| `scripts/notifications.sh` | assert the generic webhook fires start + result with the templated payload and auth header |
| `scripts/api-surface.sh` | assert the read-only HTTP surface to contract: the `/livez` + `/readyz` probes, version/config (secrets redacted), task-list filters + invalid-status 400, unknown-task 404, deploy-lock POST/DELETE 404 when OIDC auth is off |
| `scripts/read-auth.sh` | assert OIDC read protection: toggles `OIDC_ENABLED` on the release with the issuer pointed at a closed port and reverts, asserting 401 without a credential, 503 (never 401) when the provider cannot be consulted — including with a rejected JWT alongside, 15× because strategy order is randomized — 200 for a deploy token / JWT, the `/tasks/:id`, `/config`, `/livez`, `/readyz`, `/metrics` and `POST /tasks` exemptions, and the `unauthenticated_reads` counter semantics. Also asserts the /ws handshake: 401 with no credential, accepted with a deploy token, and 503 for a subprotocol-borne token whose provider is unreachable. Needs no identity provider; group-based authorization is covered by the Keycloak integration suite instead |
| `scripts/client-knobs.sh` | assert client env knobs via the real client: `TASK_REFRESH=false` still deploys, `DEBUG=true` cURL log redacts the deploy token |
| `scripts/jwt-auth.sh` | assert the JWT (`BEARER_TOKEN`) auth path: mint an HS256 token, deploy with no deploy token, prove the authenticated write-back reaches deployed. Then sets `JWT_ISSUER` + `JWT_AUDIENCE` on the release and reverts, asserting a claimless token turns 401, a matching one 202, one from another issuer 401, and that unsetting them restores the claimless token. Finally asserts the `allowed_apps` claim: a token carrying it deploys only the applications it names (401 elsewhere, naming the claim), while one omitting it still authorizes every application |
| `tools/mintjwt/` | tiny Go HS256 JWT minter (signs with the server's own jwt library; avoids an openssl dependency). `JWT_ISS` / `JWT_AUD` / `JWT_ALLOWED_APPS` add those claims |
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
| `scripts/shutdown-drain.sh` | assert graceful shutdown: hold WebSocket clients open, delete the pod, and assert readiness fails before the listener closes (logged, and the sequence never finishes faster than the 5s propagation window), every client sees a `1001 "server shutdown"` close, and the logs show the ordered drain with no data race / panic / drain timeout |
| `scripts/argocd-unreachable.sh` | assert the ArgoCD-unreachable signal (#498): scale `argocd-server` to 0, assert `GET /reachability` flips to `{"available":false,"reason":"argocd"}` (state backend stays up), the watcher broadcasts `"argocd_down:argocd"`, and `POST /tasks` fast-fails `503 {"status":"down"}` (well under the retry budget); then scale back up and assert recovery (`available:true`, `"argocd_up"`, `202`) |
| `tools/wsprobe/` | tiny Go WebSocket probe used by `lockdown` (grep for the `"locked"` broadcast), `shutdown-drain` (assert the graceful GoingAway close), and `argocd-unreachable` (grep for `"argocd_down:argocd"`/`"argocd_up"`), streaming `MSG`/`CLOSED` events one per line |

## Gotchas (why the scripts exist)

- **`wait_service` gates on `/livez`, never `/readyz`.** Readiness is legitimately
  503 while the server drains or its state backend is unreachable, so a phase that
  induces either would hang on a readiness gate. Liveness answers 200 whenever the
  process is serving, which is what the callers actually wait for.
- **The lab overrides the chart's probe paths** (`values/argo-watcher.yaml`). The
  chart still defaults both probes to the removed `/healthz`; without the override
  every pod fails both probes and the lab never comes up.
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
  (not failed) if a slow rollout boots in-window. A manual lock/unlock is not a
  separate trigger — the API handlers do not push, so it reaches clients through
  the same watcher poll; `state-postgres` asserts that over the WebSocket.
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
- **`app-tokens` needs BOTH OIDC and Postgres, so it runs after `state-postgres`.**
  Application deploy tokens are registered only when `OIDC_ENABLED=true` *and*
  `STATE_TYPE=postgres` — a token outlives the process holding it, so there is no
  in-memory store. The phase therefore inherits the Postgres release the previous
  phase left, layers `OIDC_ENABLED` + the lab's Keycloak on top for its own
  duration, and reverts to Postgres-without-OIDC (`AW_EXTRA_VALUES` in `lib.sh` is
  what keeps `helm_apply_aw`'s revert from silently dropping the overlay). The
  complementary case — OIDC on but in-memory state, where the endpoints must still
  be absent — is asserted in `read-auth.sh`, the lab's only OIDC-on in-memory
  server.
- **Keycloak's issuer is pinned to its in-cluster URL, not the NodePort.** The
  phase mints tokens from the host over `localhost:30500` while argo-watcher runs
  discovery and userinfo from inside the cluster. Left to derive the issuer per
  request, Keycloak would mint a token on one hostname that the userinfo endpoint
  rejects on the other; `KC_HOSTNAME=http://keycloak.keycloak` with
  `KC_HOSTNAME_STRICT=false` makes both paths agree. The token request also asks
  for the `openid` scope — Keycloak 26 answers userinfo `403` without it.
- **`shutdown-drain` follows the pod's logs before deleting it.** A recreated
  StatefulSet pod is a new object, so `kubectl logs --previous` would not have the
  terminated instance's shutdown logs; the script streams `logs -f` to a file
  first, then deletes the pod with `--grace-period=60` so the whole drain (the
  server's fixed 25s shutdown budget: readiness delay, HTTP, WebSockets, then the
  git write-back) completes before SIGKILL.
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
