#!/usr/bin/env bash
# Assert the freshly-installed argo-watcher is up and actually talking to the real
# ArgoCD: /healthz is 200 (it returns 503 when ArgoCD is unreachable) and the
# argocd_unavailable gauge is 0. Run right after `task up` as the gate that the lab
# is wired correctly before any phase drives a deploy.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

wait_service || die "argo-watcher never answered on ${AW_URL}"
# The webhook-tester NodePort is otherwise first used by the notifications phase, ~20
# minutes in; prove its selector resolves here instead of failing late.
wait_url "${WHT_URL}/healthz" || die "webhook-tester not reachable on ${WHT_URL} (check the nodeports selector)"

code=$(curl -s -m 10 -o /dev/null -w '%{http_code}' "${AW_URL}/healthz")
# metric_raw, not metric_sum: an ABSENT gauge must fail this gate, not read as 0.
unavail=$(metric_raw argocd_unavailable)
echo "healthz=${code} argocd_unavailable=${unavail:-<absent>}"

[[ "$code" == "200" ]]  || die "healthz=${code}, want 200"
[[ "$unavail" == "0" ]] || die "argocd_unavailable=${unavail:-<absent>}, want 0 (not reaching ArgoCD)"
echo "VERIFY: PASS"
