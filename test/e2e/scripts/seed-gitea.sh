#!/usr/bin/env bash
# Seed Gitea for the e2e lab and wire argo-watcher's SSH write-back:
#   1. create org `e2e` + public repo `gitops`, push the fixture chart (HTTP)
#   2. generate an SSH keypair; register the public key as a write deploy key
#   3. store the private key in Secret argo-watcher/argo-watcher-ssh
#   4. capture Gitea's SSH host key into ConfigMap argo-watcher/e2e-ssh-known-hosts
#
# Argo reads the repo over HTTP (public, no creds); argo-watcher writes back over
# SSH with the deploy key. Idempotent: safe to re-run.
set -euo pipefail

ORG="${ORG:-e2e}"
REPO="${REPO:-gitops}"
NS_AW="${NS_AW:-argo-watcher}"
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"
HTTP_PORT="${HTTP_PORT:-13000}"
# known_hosts entry label for Gitea SSH. Bracketed [host]:port form because
# Gitea's rootless SSH binds 2222 (non-standard). Must match the host:port in
# the write-back-repo annotation in fixtures/application.yaml.tmpl.
SSH_HOST="[gitea-ssh.gitea.svc.cluster.local]:2222"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
chart_src="${here}/../fixtures/chart"
cronjob_src="${here}/../fixtures/cronjob"
multiimage_src="${here}/../fixtures/multi-image"
rollout_src="${here}/../fixtures/rollout-chart"
work="$(mktemp -d)"
trap 'rm -rf "$work"; kill $(jobs -p) 2>/dev/null || true' EXIT

# --- port-forward (HTTP API + git push) --------------------------------------
kubectl -n gitea port-forward svc/gitea-http "${HTTP_PORT}:3000" >/dev/null 2>&1 &
api="http://localhost:${HTTP_PORT}/api/v1"
ready=false
for _ in $(seq 1 30); do
  curl -sf -m 3 -u "${GITEA_ADMIN}:${GITEA_PW}" "${api}/version" >/dev/null 2>&1 && { ready=true; break; }
  sleep 1
done
[[ "$ready" == true ]] || { echo "gitea API never became ready" >&2; exit 1; }

gapi() { curl -sf -m 10 -u "${GITEA_ADMIN}:${GITEA_PW}" -H 'Content-Type: application/json' "$@"; }

# --- org + repo (ignore "already exists") ------------------------------------
gapi -X POST "${api}/orgs" -d "{\"username\":\"${ORG}\"}" >/dev/null 2>&1 || true
gapi -X POST "${api}/orgs/${ORG}/repos" \
  -d "{\"name\":\"${REPO}\",\"private\":false,\"auto_init\":false}" >/dev/null 2>&1 || true

# --- SSH keypair (RSA PEM: parsed by go-git without surprises) ---------------
ssh-keygen -t rsa -b 4096 -m PEM -N '' -C 'argo-watcher-e2e' -f "${work}/id_rsa" >/dev/null

# register public key as a WRITE deploy key (delete stale one first)
for id in $(gapi "${api}/repos/${ORG}/${REPO}/keys" | jq -r '.[]|select(.title=="argo-watcher")|.id' 2>/dev/null); do
  gapi -X DELETE "${api}/repos/${ORG}/${REPO}/keys/${id}" >/dev/null 2>&1 || true
done
gapi -X POST "${api}/repos/${ORG}/${REPO}/keys" \
  -d "{\"title\":\"argo-watcher\",\"read_only\":false,\"key\":\"$(cat "${work}/id_rsa.pub")\"}" >/dev/null

# --- push the fixture chart over HTTP ----------------------------------------
git -C "$work" init -q -b main
mkdir -p "${work}/chart"
cp -r "${chart_src}/." "${work}/chart/"
# Vendor the `app` dependency into the disposable repo so ArgoCD's repo-server
# renders it without needing network access at sync time. The resolved chart is
# committed only here (ephemeral Gitea), never in the argo-watcher repo.
helm dependency update "${work}/chart" >/dev/null
# Plain-manifest CronJob for the fire-and-forget fixture (ffapp), deployed by a
# directory-type Argo source — no Helm, so it is pushed as-is alongside the chart.
mkdir -p "${work}/cronjob"
cp -r "${cronjob_src}/." "${work}/cronjob/"
# Two-image umbrella for the multi-image fixture (multiapp); vendor its `app`
# dependency the same way as the main chart.
mkdir -p "${work}/multi-image"
cp -r "${multiimage_src}/." "${work}/multi-image/"
helm dependency update "${work}/multi-image" >/dev/null
# Rollout chart for the accept-suspended fixture (suspendapp); no subchart deps,
# so it is pushed as-is (no helm dependency update).
mkdir -p "${work}/rollout-chart"
cp -r "${rollout_src}/." "${work}/rollout-chart/"
git -C "$work" -c user.name=seed -c user.email=seed@e2e add chart cronjob multi-image rollout-chart
git -C "$work" -c user.name=seed -c user.email=seed@e2e commit -qm 'seed fixture chart, cronjob, multi-image, and rollout charts'
git -C "$work" push -q --force \
  "http://${GITEA_ADMIN}:${GITEA_PW}@localhost:${HTTP_PORT}/${ORG}/${REPO}.git" main

# --- k8s secret (private key) + known-hosts configmap ------------------------
kubectl create namespace "$NS_AW" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS_AW" create secret generic argo-watcher-ssh \
  --from-file=sshPrivateKey="${work}/id_rsa" \
  --dry-run=client -o yaml | kubectl apply -f -

# Read Gitea's SSH host public key straight from the pod (its built-in Go SSH
# server does not answer ssh-keyscan cleanly through a port-forward). The key is
# per-server, so we prefix it with the in-cluster hostname argo-watcher uses.
gitea_pod="$(kubectl -n gitea get pod -l app.kubernetes.io/name=gitea -o name | head -1)"
hostkey="$(kubectl -n gitea exec "$gitea_pod" -- cat /data/ssh/gitea.rsa.pub)"
[[ -n "$hostkey" ]] || { echo "failed to read gitea host key" >&2; exit 1; }
kubectl -n "$NS_AW" create configmap e2e-ssh-known-hosts \
  --from-literal=ssh_known_hosts="${SSH_HOST} ${hostkey}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "seeded ${ORG}/${REPO}, deploy key + ssh secret + known-hosts ready"
