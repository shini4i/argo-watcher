#!/usr/bin/env bash
# Generate the OpenAPI spec into web/public/swagger/ and copy it into docs/reference/.
# Usage: ./docs/scripts/build-swagger.sh (requires only go on PATH; swag runs via `go run`).

set -euo pipefail

SWAG_MODULE="github.com/swaggo/swag/cmd/swag@v1.16.6"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SPEC_OUT_DIR="${REPO_ROOT}/web/public/swagger"
DOCS_OUT="${REPO_ROOT}/docs/reference/swagger.json"

cd "${REPO_ROOT}/cmd/argo-watcher"

echo "===> Generating swagger spec"
# Each annotated package is listed instead of --parseDependency, which walks $GOROOT and
# fails when the stdlib is newer than the Go swag was built with.
go run "${SWAG_MODULE}" init \
  --parseInternal \
  --dir ./,../../internal/server,../../internal/models,../../internal/config \
  --outputTypes json \
  -o "${SPEC_OUT_DIR}"

# Without the swaggertype tag on ServerConfig, swag documents argo_cd_url as a url.URL struct.
if ! grep -A1 '"argo_cd_url":' "${SPEC_OUT_DIR}/swagger.json" | grep -q '"type": "string"'; then
  echo "===> ERROR: argo_cd_url is not documented as a string (check the swaggertype tag)" >&2
  exit 1
fi

echo "===> Copying swagger.json into docs/reference/"
cp "${SPEC_OUT_DIR}/swagger.json" "${DOCS_OUT}"

echo "===> Done: ${DOCS_OUT}"
