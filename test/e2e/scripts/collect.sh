#!/usr/bin/env bash
# Collect + assert soak signals. Fails (exit 1) if any gate trips:
#   - any DATA RACE in an argo-watcher pod log
#   - failed_deployment metric != 0
#   - a lost update: a fixture app's committed image tag != the last tag the
#     driver deployed to it (per the driver summary JSON)
#   - any failed task in the driver summary
#
# Usage: collect.sh <driver-summary.json>
set -uo pipefail

SUMMARY="${1:?usage: collect.sh <driver-summary.json>}"
fail=0

echo "=== race detector ==="
races=0
for p in $(kubectl -n argo-watcher get pods -o name); do
  c=$(kubectl -n argo-watcher logs "$p" 2>/dev/null | grep -c 'DATA RACE')
  echo "  $p: $c"
  races=$((races + c))
done
[ "$races" -eq 0 ] || { echo "FAIL: $races DATA RACE line(s)"; fail=1; }

echo "=== metrics ==="
kubectl -n argo-watcher port-forward svc/argo-watcher 18091:80 >/dev/null 2>&1 &
pf=$!
sleep 3
metrics="$(curl -s -m 10 localhost:18091/metrics)"
kill $pf 2>/dev/null || true
# Some metrics are per-app labeled (failed_deployment{app=..}, processed_..);
# sum the value field ($NF) across all series of each.
sum_metric() { echo "$metrics" | awk -v k="^$1[ {]" '$0 ~ k {s+=$NF} END{print s+0}'; }
fd=$(sum_metric failed_deployment)
pd=$(sum_metric processed_deployments)
au=$(echo "$metrics" | awk '/^argocd_unavailable /{print $NF}')
ip=$(echo "$metrics" | awk '/^in_progress_tasks /{print $NF}')
echo "  failed_deployment=${fd} processed_deployments=${pd} argocd_unavailable=${au:-?} in_progress_tasks=${ip:-?}"
[ "${fd:-0}" = "0" ] || { echo "FAIL: failed_deployment=${fd}"; fail=1; }
[ "${au:-0}" = "0" ] || { echo "FAIL: argocd_unavailable=${au}"; fail=1; }

echo "=== no lost updates ==="
kubectl -n gitea port-forward svc/gitea-http 13001:3000 >/dev/null 2>&1 &
gpf=$!
sleep 3
work="$(mktemp -d)"
# Credentials built from vars (not an inline user:pass@ URL) — matches
# seed-gitea.sh and keeps a throwaway lab password out of a basic-auth URL literal.
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"
git clone -q "http://${GITEA_ADMIN}:${GITEA_PW}@localhost:13001/e2e/gitops.git" "${work}/r" 2>/dev/null
kill $gpf 2>/dev/null || true
for app in $(jq -r '.last_tag | keys[]' "$SUMMARY"); do
  want=$(jq -r ".last_tag[\"${app}\"]" "$SUMMARY")
  got=$(awk '/value:/{print $2}' "${work}/r/chart/.argocd-source-${app}.yaml" 2>/dev/null | tr -d '"')
  if [ "$got" = "$want" ]; then
    echo "  ${app}: OK (${got})"
  else
    echo "  ${app}: LOST UPDATE want=${want} got=${got:-<none>}"; fail=1
  fi
done

echo "=== task tallies ==="
jq '{submitted,deployed,failed,other}' "$SUMMARY"
tf=$(jq -r '.failed' "$SUMMARY")
[ "${tf}" = "0" ] || { echo "FAIL: ${tf} failed task(s)"; fail=1; }

if [ "$fail" -eq 0 ]; then echo "COLLECT: PASS"; else echo "COLLECT: FAIL"; exit 1; fi
