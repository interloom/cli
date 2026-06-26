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

`spaces`, `cases`, `notes`, `procedures` and `agents` share the same verbs:

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
interloom agents update <id> --model gpt-5
```

Raw JSON still works via `--data/-d` (inline), `--file/-f` (a path, or `-` for
stdin), or piped stdin — use it for fields without a flag (e.g. a procedure's
`stages`). Field flags and a raw body are mutually exclusive.

```sh
interloom cases create -d '{"title":"New case"}'
interloom cases update <id> -f patch.json
```

`agents` has no `delete`. `users` is read-only (`list`, `get`) and adds `me`.

### Listing and pagination

```sh
interloom cases list --space-id <id> --sort created_at --direction desc
interloom cases list --parent-case-id <id> --sort position --direction asc
interloom cases list --status open --status started   # repeat for multiple statuses
interloom cases list --limit 50 --cursor <next_cursor>
interloom notes list --all          # fetch every page into one list
```

Available list filters per resource:

| Resource     | Filters                                                       |
| ------------ | ------------------------------------------------------------- |
| `spaces`     | —                                                             |
| `cases`      | `space-id`, `parent-case-id`, `assignee-id`, `status` (repeatable), `sort`, `direction` |
| `notes`      | `space-id`, `case-id`, `thread-id`, `sort`, `direction`       |
| `procedures` | `space-id`                                                    |
| `files`      | `space-id`, `case-id`, `sort`, `direction`                    |

## Files

Files use the shared `list`/`get`/`update`/`delete` plus `upload` and `download`:

```sh
interloom files upload ./report.pdf --space-id <id>
interloom files download <id> --out ./report.pdf
interloom files download <id> > report.pdf      # stream to stdout
```

## Users

```sh
interloom users me        # the authenticated user
interloom users list
interloom users get <id>
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
interloom threads messages create <id> -d '{"text":"Hello from JSON"}'
```

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
