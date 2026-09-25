# interloom

An agent-first command line interface for the Interloom REST API.

Output is JSON by default. Errors are a JSON envelope on stderr with stable exit
codes (auth, not found, validation, usage, generic), so the CLI is easy to drive
from scripts and agents.

## Install

```sh
# npm (installs the right prebuilt binary for your platform)
npm i -g @interloom/cli

# or with Go
go install github.com/interloom/cli@latest
```

Or grab a prebuilt binary from the [releases](https://github.com/interloom/cli/releases).

## Authentication

`auth login` stores a long-lived API key for an instance. It defaults to
`app.interloom.com`; pass another instance as an argument (a short name like
`dev`, a host like `dev.interloom.com`, or a local address like `localhost:8080`,
which always uses http).

The API key is **never** read from a flag. It comes from piped stdin, then
`INTERLOOM_API_KEY`, then a hidden interactive prompt. When prompting, the
instance's personal-tokens page is opened in your browser so you can create a
key, then paste it back. Pass `--organization-slug` to open a specific
organization's page (`/<slug>/personal-tokens`):

```sh
# Pipe the key (recommended for CI / agents)
echo "$MY_KEY" | interloom auth login dev

# Opens <base-url>/personal-tokens to create a key, then prompts for it
interloom auth login

# Verify the credentials and show the authenticated user and organization
interloom auth status
```

Each saved set of credentials is a **config**, identified by the instance host
and the key's organization and named `<host>-<org>` (e.g. `dev-acme`), so the
same host can hold several organizations side by side. Configs live in
`~/.config/interloom/<config-name>.json`; the current one is tracked in
`~/.config/interloom/config.json`.

### Switching configs

```sh
interloom config list              # list saved configs (marks the current one)
interloom config use dev-acme      # set the current config
interloom config current           # print the current config
interloom config delete dev-acme   # remove a saved config (does not revoke the key)
```

### Environment overrides

These always override the saved config:

| Variable             | Purpose                          |
| -------------------- | -------------------------------- |
| `INTERLOOM_API_KEY`  | API key                          |
| `INTERLOOM_BASE_URL` | API base URL                     |
| `INTERLOOM_CONFIG`   | config to use                    |

You can also override per-invocation with `--config-name/-c` and `--base-url`.

## Resources

`spaces`, `cases`, `notes`, `procedures`, `agents`, and `tools` use the standard
resource commands supported by their API endpoints:

```sh
interloom cases list
interloom cases get <id>
interloom cases create --title "New case" --description "Details"
interloom cases update <id> --status completed
interloom cases delete <id>
```

`create`/`update` accept the body either as **typed field flags** or as raw
JSON. The common fields are exposed as flags (run `<resource> create --help` to
see them); repeatable fields like `--tags` take a comma-separated list or repeat
the flag. Required-on-create fields are marked `(required)` in help.

```sh
interloom notes create --title "Note" --body "..." --space-id <id> --tags a,b
interloom agents update <id> --model gpt-5 --reasoning-effort HIGH
```

Raw JSON still works via `--data/-d` (inline), `--file/-f` (a path, or `-` for
stdin), or piped stdin — use it for fields without a flag (e.g. a procedure's
`stages`). Field flags and a raw body are mutually exclusive.

```sh
interloom cases create -d '{"title":"New case"}'
interloom cases update <id> -f patch.json
```

`agents` and `tools` have no `delete`. `users` is read-only (`list`, `get`) and
adds `me`. `models` is list-only for discovering model IDs accepted by agent
commands.
`secrets` supports `list`, `create`, and `delete`; secret values are never
returned by the API.
`case-ingestions` imports cases from JSONL manifest files and exposes ingestion
status plus failed-entry pagination.
`databases` describes database schemas, queries bounded pages of selected
columns, calculates aggregate values, and adds or replaces rows. It also
supports `list`, `create`, and `delete`, but not `update`.

### Listing and pagination

```sh
interloom cases list --space-id <id> --sort created_at --direction desc
interloom cases list --parent-case-id <id> --sort position --direction asc
interloom cases list --status open --status started   # repeat for multiple statuses
interloom cases list --limit 50 --cursor <next_cursor>
interloom notes list --all          # fetch every page into one list
```

Unscoped `cases list` sends `sort=created_at&direction=desc` by default. Lists
scoped to a Space or parent Case send `sort=position&direction=asc` for
case-tree browsing.

Available list filters per resource:

| Resource     | Filters                                                       |
| ------------ | ------------------------------------------------------------- |
| `spaces`     | —                                                             |
| `cases`      | `space-id`, `parent-case-id`, `assignee-id`, `status` (repeatable), `sort`, `direction` |
| `notes`      | `space-id`, `case-id`, `thread-id`, `sort`, `direction`       |
| `procedures` | `space-id`                                                    |
| `databases`  | `space-id` (required)                                         |
| `models`     | —                                                             |
| `tools`      | —                                                             |
| `secrets`    | —                                                             |
| `files`      | `space-id`, `case-id`, `sort`, `direction`                    |

### Relationships

List the resources connected to a Space, Case, Note, Procedure, Agent, File, or
User. Relationship lists use the same cursor pagination flags as other lists.

```sh
interloom cases relationships <case-id>
interloom cases relationships <case-id> --limit 50 --cursor <next_cursor>
interloom cases relationships <case-id> --all
```

Each item contains the linked resource `id`, `type`, and `url`. When available,
it also contains `relationship_type` and `relationship_direction` relative to
the requested resource.

### Space triggers

Get or update the triage trigger applied to new cases in a Space. Updates use
the trigger JSON shape from the API schema; set `trigger_type` to `null` to
disable the trigger.

```sh
interloom spaces trigger get <space-id>
interloom spaces trigger update <space-id> -d '{"trigger_type":"assignee","assignee_id":"<user-or-agent-id>"}'
interloom spaces trigger update <space-id> -d '{"trigger_type":null}'
```

### Space members

List the users with access to a Space, add or update a membership, or remove a
member. Membership roles accepted by writes are `member`, `manager`, and
`viewer`.

```sh
interloom spaces members list <space-id>
interloom spaces members add <space-id> <user-id> --role viewer
interloom spaces members remove <space-id> <user-id>
```

### Databases

List databases in one space with `databases list --space-id <space-id>`.
The list supports `--limit`, `--cursor`, and `--all` without loading rows.
Create a database with `databases create -f database.json`, using this body:

```json
{"space_id":"<space-id>","key":"example_records","title":"Example records","schema":{"columns":[{"name":"row_id","type":"string"},{"name":"status","type":"string","nullable":true}],"row_key_column":"row_id"}}
```

The schema is immutable and accepts at most 100 columns. Its row-key column
must be a non-nullable string. Creation with a compatible schema and the same
space-local key returns the existing database without changing its title,
rows, or revision. An incompatible schema returns `database_schema_conflict`.
The scalar fields have `--space-id`, `--key`, and `--title` flags, but the
required nested `schema` needs a complete raw JSON body (`--data`, `--file`,
or stdin). Raw JSON and field flags cannot be combined.

Use `databases delete <database-id>` to permanently delete a database, its
rows, and its write-idempotency records. There is no revision precondition.

Describe a database without loading rows, query up to 100 rows from selected
columns, or calculate up to 10 named aggregate values. Query and aggregate
requests accept the JSON body through `--data`, `--file`, or stdin. Equality
filters are optional. Queries also support one scalar sort. Pass `next_cursor`
back as `cursor` in the next query request; restart without a cursor if the
database revision changes.

```sh
interloom databases get <database-id>
interloom databases query <database-id> -d '{"selected_columns":["row_id","status"],"page_size":100}'
interloom databases query <database-id> -f query.json
interloom databases aggregate <database-id> -d '{"expressions":[{"name":"rows","function":"count"},{"name":"total","function":"sum","column":"amount"}]}'
```

Use `databases upsert <database-id> -f batch.json` to add or fully replace rows.
The command also accepts `--data` or stdin. Fetch the schema and revision with
`databases get` first. The body must contain `expected_revision` and `rows`:

```json
{"expected_revision":4,"rows":[{"row_id":"example-1","status":"open"}]}
```

Use the actual revision and declared column names from your database. Each row
must include its key and all required columns. Omitted optional values become
null; this is not a partial update. Send decimals, UUIDs, and timestamps as
strings, with a timezone for timestamps. Aim for 100–250 rows per batch. The
limits are 1,000 rows per request, 64 KiB per row, and 5 MiB per batch (compact,
normalized UTF-8 JSON). A database can hold up to 100,000 rows.

Each new batch, including an empty batch, advances the revision once. Use the
returned `committed_revision` for the next batch. If a response is lost, retry
the same rows with the original `expected_revision` to get the original result.
On `write_conflict`, read the latest data and revise the batch before retrying.

## Files

Files use the shared `list`/`get`/`update`/`delete` plus `upload` and `download`:

```sh
interloom files upload ./report.pdf --space-id <id>
interloom files download <id> --out ./report.pdf
interloom files download <id> > report.pdf      # stream to stdout
```

## Case ingestions

```sh
interloom case-ingestions create ./manifest.jsonl --space-id <id>
interloom case-ingestions get <id>
interloom case-ingestions errors <id> --limit 50
interloom case-ingestions errors <id> --all
```

## Users

```sh
interloom users me        # the authenticated user
interloom users list
interloom users get <id>
```

## Agent tools

List or replace the complete set of tools assigned to an Agent:

```sh
interloom agents tools list <agent-id>
interloom agents tools replace <agent-id> --tool-ids <tool-id-1>,<tool-id-2>
interloom agents tools replace <agent-id> -d '{"tool_ids":[]}'
```

## Custom tools

Create and update custom tools with the standard `tools` commands. A tool's
`input_schema` is a nested JSON object, so create requests should use a JSON
body; scalar and repeatable update fields are also available as flags.

```sh
interloom tools create -f tool.json
interloom tools update <tool-id> --description "Updated description"
interloom tools update <tool-id> --secret-ids <secret-id-1>,<secret-id-2>
```

## Secrets

Create, list, or delete organization secrets. The API never returns stored
secret values.

```sh
interloom secrets create --name EXAMPLE_API_TOKEN --value "$EXAMPLE_API_TOKEN"
interloom secrets list
interloom secrets delete <id>
```

## Models

```sh
interloom models list
```

## Threads

Threads have no collection list. `get` fetches a single thread, `events` lists
its event stream with cursor pagination, and `messages create` posts a message:

```sh
interloom threads get <id>
interloom threads events <id> --limit 50 --direction desc
interloom threads events <id> --cursor <next_cursor>
interloom threads events <id> --all      # fetch every page into one list
interloom threads messages create <id> --text "Hello from the CLI"
interloom threads messages create <id> --text "See attached" --file-ids <file-id>
interloom threads messages create <id> -d '{"text":"Hello from JSON"}'
```

Thread event payloads can also contain `agent_invocation` links. Use the
linked invocation ID to inspect an agent execution.

## Invocations

Invocations are read-only agent executions, not individual LLM turns. There is
no collection list or write command. Discover IDs through thread events:

```sh
interloom invocations get <id>
interloom invocations steps <id> --limit 50
interloom invocations steps <id> --cursor <next_cursor>
interloom invocations steps <id> --all
```

Steps are ordered by creation time and ID. Tool-call steps include raw inputs
and outputs, which may contain sensitive data. Thinking and reasoning-summary
steps expose metadata only; their content is withheld. Final responses remain
in thread messages. Invocations and steps include nullable `token_usage` counts.
Metrics can arrive late. Cache-read and cache-write counts are included in input
tokens, not additional tokens. Step counts are allocated shares of an LLM response;
visible steps can exclude final or unsaved steps, so their sum can differ from the
invocation total. Per-step costs are not exposed.

## Usage

```sh
interloom cases usage <case-id>
interloom spaces usage <space-id>
interloom spaces usage breakdowns <space-id> --group-by case
interloom spaces usage breakdowns <space-id> --group-by model --limit 20
interloom spaces usage breakdowns <space-id> --group-by agent
```

Usage covers all time; date filters are not available. Case totals include
descendants. Space totals and breakdowns require owner or manager access.
Breakdowns require `--group-by case|model|agent`. Groups are ordered by group key,
not usage. Omit `--limit` for all groups, or supply an integer of at least 1.
There are no `--cursor` or `--all` flags for these commands.

Metrics can arrive late. With no metrics, counts are zero and token totals and
averages are null. The CLI preserves the API values without calculating totals.
Cache reads are included in input tokens. Interactions count distinct LLM calls
plus tool-call steps. Costs are customer costs in EUR; they can be incomplete or
null when customer pricing is disabled or no priced usage exists.

Case groups use the recorded root case and its space. Model and agent groups,
like Space totals, use the invocation's recorded space. Thus, case breakdown
totals can differ from Space totals. Deleted cases and agents can remain in groups.
These usage commands are not exposed as MCP tools.

## MCP server

`mcp` runs a Model Context Protocol server that exposes the CLI's API operations
as MCP tools. It uses stdio by default, so MCP clients can launch it directly as
`interloom mcp`. API calls use your saved CLI config token and the normal
environment overrides.

```sh
interloom auth login              # if you have not saved credentials yet
interloom mcp
```

Pass `--http` to serve Streamable HTTP instead of stdio. The HTTP endpoint has
no MCP auth because it only binds to loopback addresses.

```sh
interloom mcp --http
# MCP endpoint: http://127.0.0.1:8765/mcp

interloom mcp --http --addr 127.0.0.1:9000 --config-name dev-acme
```

For safety, HTTP `--addr` must be `localhost` or a loopback IP. Use `--endpoint`
with `--http` to change the HTTP path if your MCP client expects a different one.

## Version

```sh
interloom version
```

## Development

```sh
./scripts/generate.sh     # pull the latest OpenAPI spec and regenerate models
go build ./...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
```

## License

[MIT](LICENSE) © Interloom Technologies GmbH
