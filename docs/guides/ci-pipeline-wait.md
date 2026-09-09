# Wait for an Argo CD Deployment from CI

Your pipeline builds an image, pushes it, and commits the new tag to the GitOps repository. Argo CD deploys it. The pipeline finishes long before the rollout does, so it cannot tell you whether the image you built is running.

This guide compares the ways to close that gap and explains when Argo Watcher is the right one.

## The options

| Approach | What it waits for | Knows which image you built | Needs in every pipeline |
|---|---|---|---|
| `argocd app wait` | The Application to be `Synced` and `Healthy` | No | The `argocd` CLI and an Argo CD account token |
| Polling the Argo CD API | Whatever you script — usually the same two states | Only if you write the check yourself | An Argo CD account token and the polling script |
| `kubectl rollout status` | One workload's current rollout to finish | No | Cluster credentials, which the GitOps pull model exists to avoid |
| Argo Watcher client | The Application to be `Synced` and `Healthy` **with your image tag among its running images** | Yes | The server URL and, optionally, a deploy token |

## Why not `argocd app wait`?

`argocd app wait` is the right tool when the pipeline itself runs `argocd app sync` and owns the deployment end to end. In a GitOps setup the pipeline only commits, and the command falls short in three ways.

**It does not know about your image.** The command takes an application name and waits for `Synced` and `Healthy`. Its flags select *which* conditions to wait for (`--sync`, `--health`, `--operation`, `--suspended`, `--degraded`, `--delete`, `--hydrated`) and *which* resources to watch (`--resource`, `--selector`). None of them names a revision or an image tag. If Argo CD has not noticed your commit yet, the application is still `Synced` and `Healthy` on the previous revision, that condition is already met, and the command returns success for a deployment that has not started.

**It reports a state, not an outcome.** When it does time out, all you have is an exit code in one pipeline log. Argo Watcher records every deployment as a task with its final status and reason, so the outcome outlives the pipeline run and sits in the Web UI next to the deployments before and after it.

**It lives in the pipeline.** Every project's CI needs the `argocd` binary and an Argo CD token with read access to its applications. Argo Watcher holds the single Argo CD token; pipelines talk to Argo Watcher with a URL and an optional [deploy token](../reference/api.md#authentication).

The same three points apply to a hand-written polling loop against the Argo CD API, plus the cost of maintaining it.

## What Argo Watcher checks

A task names the application, the images, and the tag the pipeline expects. The server polls Argo CD until it can report an outcome to the client, which exits with a matching status code. The three you will meet most often:

- `deployed` — the application is `Synced` and `Healthy` and the requested images carry the requested tag.
- `failed` — Argo CD reported a health or sync failure, the deployment timed out, or the application rolled out without the requested image.
- `cancelled` — a newer deployment of one of the same images superseded this one. The metrics do not count it as a deployment failure, but the client still exits non-zero.

The full list of states is in [Concepts](../getting-started/concepts.md#task-lifecycle).

Two opt-ins relax the check. The [`argo-watcher/fire-and-forget`](../reference/annotations.md) annotation marks a task `deployed` without monitoring the rollout, and [`ACCEPT_SUSPENDED_APP`](../reference/server-env.md#core) treats a `Synced` application whose health is `Suspended` as deployed.

Beyond the exit code, every task is kept as history and shown in the Web UI, can trigger [notifications](notifications.md), is blocked by the [deployment lock](deployment-lock.md), and can carry the image-tag commit itself through the [GitOps Updater](gitops-updater.md).

## Minimal pipeline step

The client is a container image that reads everything from the environment ([full list](../reference/client-env.md)). A GitLab CI job:

```yaml
watch:
  image: ghcr.io/shini4i/argo-watcher-client:<VERSION>
  variables:
    ARGO_WATCHER_URL: https://argo-watcher.example.com
    ARGO_APP: example
    IMAGES: $CI_REGISTRY_IMAGE
    IMAGE_TAG: $CI_COMMIT_SHORT_SHA
    COMMIT_AUTHOR: $GITLAB_USER_EMAIL
    PROJECT_NAME: $CI_PROJECT_PATH
  script:
    - /client
```

Complete GitLab CI and GitHub Actions examples, including the build step, are in [Installation](install.md#run-the-client-in-ci).
