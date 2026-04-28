#!/usr/bin/env bash
# Generate the OpenAPI spec from Go source and copy it into the docs tree
# so the mkdocs swagger-ui-tag plugin can embed it.
#
# Usage: ./docs/scripts/build-swagger.sh
#
# Requires: go on PATH; swag (github.com/swaggo/swag) on PATH OR in $(go env GOPATH)/bin.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SPEC_OUT_DIR="${REPO_ROOT}/web/public/swagger"
DOCS_OUT="${REPO_ROOT}/docs/reference/swagger.json"

if command -v swag >/dev/null 2>&1; then
  SWAG_BIN="swag"
elif [ -x "$(go env GOPATH)/bin/swag" ]; then
  SWAG_BIN="$(go env GOPATH)/bin/swag"
else
  echo "error: 'swag' binary not found on PATH or in \$(go env GOPATH)/bin" >&2
  echo "install with: go install github.com/swaggo/swag/cmd/swag@latest" >&2
  exit 1
fi

cd "${REPO_ROOT}/cmd/argo-watcher"

echo "===> Generating swagger spec via ${SWAG_BIN}"
"${SWAG_BIN}" init \
  --parseDependency \
  --parseInternal \
  --dir ./,../../internal/server \
  --outputTypes json \
  -o "${SPEC_OUT_DIR}"

echo "===> Copying swagger.json into docs/reference/"
cp "${SPEC_OUT_DIR}/swagger.json" "${DOCS_OUT}"

echo "===> Done: ${DOCS_OUT}"
