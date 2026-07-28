#!/usr/bin/env bash
# Mint an ARGO_TOKEN for argo-watcher against the freshly-installed ArgoCD and
# store it in the `argo-watcher-secret` Secret (the chart's argo.secretName).
#
# Uses an admin session token obtained via the ArgoCD API. The cluster is
# disposable and rebuilt per run, so session-token expiry (~24h) is a non-issue.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

pw="$(kubectl -n "$NS_ARGOCD" get secret argocd-initial-admin-secret \
  -o jsonpath='{.data.password}' | base64 -d)"

# Retry the session call until ArgoCD is accepting connections and answers with a
# token. -k: the lab's argocd-server serves its own self-signed cert.
token=""
mint() {
  token="$(curl -sk -m 5 "${ARGOCD_URL}/api/v1/session" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"${pw}\"}" \
    | jq -r '.token // empty' 2>/dev/null || true)"
  [[ -n "$token" ]]
}
retry 30 2 mint || die "failed to mint ARGO_TOKEN"

kubectl create namespace "$NS_AW" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS_AW" create secret generic argo-watcher-secret \
  --from-literal=ARGO_TOKEN="$token" \
  --from-literal=ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "ARGO_TOKEN + ARGO_WATCHER_DEPLOY_TOKEN stored in ${NS_AW}/argo-watcher-secret"
