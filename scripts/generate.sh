#!/usr/bin/env bash
# Regenerate the typed API models from openapi.json.
#
# oapi-codegen does not support OpenAPI 3.1 (which FastAPI emits), so we first
# downconvert the spec to 3.0 with the pure-Go tools/specconvert, then generate.
set -euo pipefail

OAPI_CODEGEN="github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.1"
# Defaults to dev; override with SPEC_URL to generate from another instance,
# e.g. SPEC_URL=http://localhost:8000/api/v1/public/openapi.json ./scripts/generate.sh
SPEC_URL="${SPEC_URL:-https://dev.interloom.com/api/v1/public/openapi.json}"
SPEC="internal/api/openapi.json"
SPEC_3_0="build/openapi.v3.0.json"

# Run from the repo root regardless of where the script is invoked.
cd "$(dirname "$0")/.."

echo "fetching latest spec from $SPEC_URL"
curl -fsSL "$SPEC_URL" -o "$SPEC"

mkdir -p build
go run ./tools/specconvert "$SPEC" "$SPEC_3_0"
go run "$OAPI_CODEGEN" -config oapi-codegen.yaml "$SPEC_3_0"

echo "generated internal/api/api.gen.go"
