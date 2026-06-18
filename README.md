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
interloom cases create -d '{"title":"New case"}'
interloom cases update <id> -f patch.json
interloom cases delete <id>
```

Request bodies for `create`/`update` come from `--data/-d` (inline JSON),
`--file/-f` (a path, or `-` for stdin), or piped stdin.

`agents` has no `delete`. `users` is read-only (`list`, `get`) and adds `me`.

### Listing and pagination

```sh
interloom cases list --space_id <id> --sort created_at --direction desc
interloom cases list --limit 50 --cursor <next_cursor>
interloom notes list --all          # fetch every page into one list
```

Available list filters per resource:

| Resource     | Filters                                                       |
| ------------ | ------------------------------------------------------------- |
| `spaces`     | —                                                             |
| `cases`      | `space_id`, `parent_case_id`, `assignee_id`, `sort`, `direction` |
| `notes`      | `space_id`, `case_id`, `thread_id`, `sort`, `direction`       |
| `procedures` | `space_id`                                                    |
| `files`      | `space_id`, `case_id`, `sort`, `direction`                    |

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
