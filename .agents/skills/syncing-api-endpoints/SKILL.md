---
name: syncing-api-endpoints
description: "Regenerates the Interloom API schema and extends the CLI to cover updated and newly added endpoints. Use when the OpenAPI spec changed, after running scripts/generate.sh, or when asked to add/update CLI commands for API resources."
---

# Syncing API endpoints

Pull the latest OpenAPI spec, regenerate the typed models, then wire any
updated or newly added endpoints into the CLI command tree.

## How the codebase fits together

- `scripts/generate.sh` fetches the spec, downconverts 3.1→3.0, and regenerates
  models. It overwrites two tracked files: `internal/api/openapi.json` (the
  spec) and `internal/api/api.gen.go` (typed models, `DO NOT EDIT`).
- `cmd/root.go` `newRootCmd()` is the single place resources are registered. A
  resource is one `newResourceCmd(resource{...})` call.
- `cmd/resource.go` generates the uniform verbs `list/get/create/update/delete`
  from a `resource` struct. You rarely touch this file.
- `internal/client/client.go` is resource-agnostic transport (one path shape
  drives every resource). Only touch it for a genuinely new transport shape
  (e.g. another multipart upload or signed-URL download).
- Special, non-uniform endpoints live in their own file: `cmd/files.go`
  (`upload`/`download`), `cmd/users.go` (read-only + `me`).
- `README.md` documents the resources, their list filters, and any special
  verbs — keep it in sync.

## Workflow

### 1. Regenerate the schema

```sh
./scripts/generate.sh
```

Defaults to the `dev` instance. Override the source if needed:
`SPEC_URL=http://localhost:8000/api/v1/public/openapi.json ./scripts/generate.sh`.

### 2. Diff the endpoint surface

Compare the committed spec against the freshly generated one so you see exactly
which paths and query params changed:

```sh
git show HEAD:internal/api/openapi.json \
  | node .agents/skills/syncing-api-endpoints/scripts/list-endpoints.js - > /tmp/before.txt
node .agents/skills/syncing-api-endpoints/scripts/list-endpoints.js internal/api/openapi.json > /tmp/after.txt
diff /tmp/before.txt /tmp/after.txt
```

Also skim `git diff internal/api/openapi.json` for changed request/response
shapes on existing endpoints (the CLI passes JSON through raw, so most body
changes need no code — but they may need a README or filter update).

### 3. Map each change to the CLI

A standard REST resource exposes a collection path (`/foo`) and an item path
(`/foo/{foo_id}`). Register it once in `newRootCmd()`:

```go
newResourceCmd(resource{name: "foo", singular: "foo", filters: []filter{...}}),
```

Set the `resource` flags from the spec:

| Spec observation                              | resource field      |
| --------------------------------------------- | ------------------- |
| Only `GET` (collection + item), no write verbs | `readOnly: true`    |
| Collection has no `POST`                       | `noCreate: true`    |
| Item has no `DELETE`                           | `noDelete: true`    |
| Collection `GET` query params (excl. paging)   | `filters: []filter` |
| Create/update request-body properties          | `fields: []field`   |

List filters come from the collection `GET` query parameters, **excluding**
`limit` and `cursor` — those are built in by `listCmd`. Reuse the shared filter
vars in `cmd/resource.go` when the names match (`filterSpaceID`, `filterCaseID`,
`filterSort`, `filterDirection`); declare inline `filter{"name", "usage"}` for
anything new, e.g. `{"parent_case_id", "filter by parent Case ID"}`.

Body fields come from the `Create*Request`/`Update*Request` schemas. Each scalar
or string-array property becomes a `field{name, usage, onCreate, onUpdate,
required, multi}` — `name` is the snake_case JSON key, the flag is kebab-case.
Set `required: true` for properties the create schema lists as required
(enforced only when the body is built from flags), `multi: true` for arrays
(emitted as a JSON array), and `onCreate`/`onUpdate` to match where the property
exists. Reuse the shared `fieldSpaceID`, `fieldCaseID`, `fieldTags` vars and the
`key*` name constants when they match. Skip nested-object properties (e.g. a
procedure's `stages`) — those stay raw-JSON-only via `--data`/`--file`. Values
pass through as raw JSON, so UUID/timestamp validation is left to the API.

Current examples to copy from `newRootCmd()`:

- `agents` → `noDelete: true` (no `DELETE` on the item).
- `users` → `readOnly: true` plus the `me` subcommand in `cmd/users.go`.
- `files` → `noCreate: true` plus `upload`/`download` in `cmd/files.go`.

### 4. Non-uniform endpoints

If a new path is not a plain collection/item pair — a sub-resource
(`/foo/{id}/bar`), an action (`/foo/{id}:run`), or anything multipart/binary —
don't force it through `resource`. Add a dedicated `cmd/<resource>.go`
modeled on `cmd/files.go`, build the subcommands with `cobra`, call the
`client` (extend it only if no existing method fits), and `printResult(raw)`.
Register it from `newRootCmd()`.

### 5. Update the README

Reflect new resources, new list filters, new create/update field flags, removed
verbs (`readOnly`/`noCreate`/`noDelete`), and any special commands in the
relevant `README.md` sections (Resources, the filters table, and per-feature
sections like Files/Users).

### 6. Verify

```sh
go build ./...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
```

Then spot-check a new command's help and, if you have credentials, a live call:

```sh
go run . <resource> --help
go run . <resource> list --limit 1
```

### 7. Smoke-test against dev

When `INTERLOOM_API_KEY` is available, exercise both read and write command
paths against the dev instance. Validate response shapes without printing
credentials or resource details. Writes are allowed on dev: use unique,
clearly named test data, clean it up when the API supports deletion, and note
any fixture that cannot be deleted.

For example, the secrets and Agent tools endpoints can be checked with:

```sh
test -n "${INTERLOOM_API_KEY:-}"
export INTERLOOM_BASE_URL=https://dev.interloom.com

secrets_json=$(go run . secrets list --limit 1)
printf '%s' "$secrets_json" \
  | jq -e '(.data | type == "array") and (.has_more | type == "boolean")' >/dev/null

agents_json=$(go run . agents list --limit 1)
printf '%s' "$agents_json" \
  | jq -e '(.data | type == "array") and (.has_more | type == "boolean")' >/dev/null

agent_id=$(printf '%s' "$agents_json" | jq -r '.data[0].id // empty')
if test -n "$agent_id"; then
  tools_json=$(go run . agents tools list "$agent_id")
  printf '%s' "$tools_json" | jq -e 'type == "array"' >/dev/null
fi
```

Also exercise changed write endpoints. Pass secret values through stdin rather
than command-line arguments, and use a dedicated Agent fixture because the API
key may not be allowed to modify an arbitrary existing Agent:

```sh
secret_name="AMP_SCHEMA_SMOKE_$(date +%s)_$$"
secret_value=$(openssl rand -hex 24)
secret_body=$(jq -nc --arg name "$secret_name" --arg value "$secret_value" \
  '{name:$name,value:$value}')
secret_json=$(printf '%s' "$secret_body" | go run . secrets create)
secret_id=$(printf '%s' "$secret_json" | jq -r '.id')
printf '%s' "$secret_json" | jq -e --arg name "$secret_name" \
  '(.id | type == "string" and length > 0) and .name == $name' >/dev/null
go run . secrets delete "$secret_id" >/dev/null

agent_name="AMP Schema Smoke $(date -u +%Y-%m-%dT%H:%M:%SZ)"
agent_body=$(jq -nc --arg name "$agent_name" '{name:$name}')
agent_json=$(printf '%s' "$agent_body" | go run . agents create)
agent_id=$(printf '%s' "$agent_json" | jq -r '.id')
current_tools=$(go run . agents tools list "$agent_id")
replace_body=$(printf '%s' "$current_tools" | jq -c '{tool_ids: map(.id)}')
replaced_tools=$(printf '%s' "$replace_body" \
  | go run . agents tools replace "$agent_id")
test "$(printf '%s' "$current_tools" | jq -c 'map(.id) | sort')" = \
  "$(printf '%s' "$replaced_tools" | jq -c 'map(.id) | sort')"
```

If a write test creates a resource and a later assertion fails, make a
best-effort cleanup call before reporting the failure. Agents currently have no
delete endpoint, so the dedicated smoke Agent remains in dev as test data.

Adapt these calls to the endpoints changed by the current schema update. Report
which calls passed or were skipped, but do not include returned IDs, names,
secret metadata, or other organization data.

## Conventions to preserve

- Output is JSON on stdout; pass API responses through raw via `printResult`.
- Never edit `internal/api/api.gen.go` by hand.
- Don't add flags for the API key (see the auth rules in `cmd/auth.go`).
- Keep `client` resource-agnostic; prefer the generic methods over per-resource
  ones.
