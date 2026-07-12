#!/usr/bin/env bash
# Mint an ARGO_TOKEN for argo-watcher against the freshly-installed ArgoCD and
# store it in the `argo-watcher-secret` Secret (the chart's argo.secretName).
#
# Uses an admin session token obtained via the ArgoCD API. The cluster is
# disposable and rebuilt per run, so session-token expiry (~24h) is a non-issue.
set -euo pipefail

NS_ARGOCD="${NS_ARGOCD:-argocd}"
NS_AW="${NS_AW:-argo-watcher}"
PORT="${PORT:-18080}"
# Deploy token gating git write-back. Only validated (authenticated) tasks
# trigger the updater push, so the driver must send this in the
# ARGO_WATCHER_DEPLOY_TOKEN header. Fixed throwaway lab value.
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"

pw="$(kubectl -n "$NS_ARGOCD" get secret argocd-initial-admin-secret \
  -o jsonpath='{.data.password}' | base64 -d)"

kubectl -n "$NS_ARGOCD" port-forward svc/argocd-server "${PORT}:443" >/dev/null 2>&1 &
pf=$!
trap 'kill $pf 2>/dev/null || true' EXIT

# Retry the session call until the port-forward is accepting connections and
# ArgoCD answers with a token.
token=""
for _ in $(seq 1 30); do
  token="$(curl -sk -m 5 "https://localhost:${PORT}/api/v1/session" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"${pw}\"}" \
    | jq -r '.token // empty' 2>/dev/null || true)"
  [ -n "$token" ] && break
  sleep 2
done
[ -n "$token" ] || { echo "failed to mint ARGO_TOKEN" >&2; exit 1; }

kubectl create namespace "$NS_AW" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS_AW" create secret generic argo-watcher-secret \
  --from-literal=ARGO_TOKEN="$token" \
  --from-literal=ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "ARGO_TOKEN + ARGO_WATCHER_DEPLOY_TOKEN stored in ${NS_AW}/argo-watcher-secret"
