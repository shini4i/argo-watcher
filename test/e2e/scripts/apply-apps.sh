#!/usr/bin/env bash
# Render and apply N fixture Argo Applications (app1..appN) from the template.
# All share the one gitops repo, so their write-back pushes contend.
#
# Usage: apply-apps.sh [N]   (default 1)
set -euo pipefail

N="${1:-1}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmpl="${here}/../fixtures/application.yaml.tmpl"

for i in $(seq 1 "$N"); do
  sed "s/__APP__/app${i}/g" "$tmpl" | kubectl apply -f -
done

echo "applied ${N} fixture application(s)"
