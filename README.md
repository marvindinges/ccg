# ccg

An interactive TUI for writing [Conventional Commits](https://www.conventionalcommits.org/),
with **optional** AI generation.
written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea) v2 and
[huh](https://github.com/charmbracelet/huh).

- **Guided commit flow:** select files → (optional) hint → (optional) AI draft →
  review hub (edit any part with a keypress, or commit) → (optional) push.
- **You always verify.** AI output is never committed without you reviewing and
  editing it in the form first.
- **Works with no AI at all (manual).** With no provider configured, ccg is a fully manual
  guided Conventional Commits tool.
- **Bring your own provider.** works with `/chat/completions` API — configurable **per project**.

## Install

### Quick install (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/install.sh | sh
```

The installer clones the repo and builds it locally, **asking before each system change**:

- installs the Go toolchain if it's missing or older than 1.21 (no sudo, into `~/.local/go`),
- clones the source into `~/.local/share/ccg/src` and builds `ccg` into `~/.local/bin`,
- adds the needed dirs to your `PATH` for **bash, zsh, and fish**,
- optionally creates a global config at `~/.config/ccg/config.yaml`.

The source checkout is kept so `ccg upgrade` can rebuild from it later.

It prints exactly what it will do and prompts for confirmation; in a non-interactive
shell it does nothing unless you pass `CCG_ASSUME_YES=1`.

```sh
# non-interactive (answers yes to everything):
curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/install.sh | CCG_ASSUME_YES=1 sh
```

Useful environment knobs: `CCG_INSTALL_DIR`, `CCG_REF` (branch/tag/commit to
build), `CCG_SRC_DIR`, `CCG_REPO_URL`, `CCG_GO_DIR`, `CCG_SKIP_PATH=1`,
`CCG_SKIP_CONFIG=1`, and `CCG_PROVIDER_BASE_URL` / `CCG_PROVIDER_MODEL` /
`CCG_PROVIDER_API_KEY_ENV` to pre-fill the config.


### Upgrade

```sh
ccg upgrade            # pull the latest source and rebuild
ccg upgrade v0.2.0     # build a specific tag/branch/commit
```

`ccg upgrade` reuses the source checkout from the installer (`~/.local/share/ccg/src`),
rebuilds, and replaces the current binary. Re-running the install script does the
same thing. (If ccg was installed another way, re-run the install command above.)

### Manual install

Requires Go 1.21+ (the toolchain auto-fetches the version pinned in `go.mod`) and `git`.

```sh
git clone https://github.com/marvindinges/ccg.git && cd ccg
go build -o ccg .
# move ./ccg onto your PATH, e.g. into ~/.local/bin
```

## Usage

Run inside a git repository:

```sh
ccg
```

Flags:

| Flag | Description |
|------|-------------|
| `--no-ai` | Force manual mode even if a provider is configured |
| `--hint "..."` | Provide the natural-language hint up front (skips the hint prompt) |
| `--all` | Pre-select all changed files for staging |
| `--push` | Push automatically after committing |
| `--no-push` | Skip the push step |
| `--dry-run` | Render the commit message and print it — don't commit |
| `--debug` | Print the resolved config and the AI request/response (troubleshooting) |

Subcommands:

```sh
ccg config        # show resolved config and where each value came from
ccg config path   # print the global and project config file paths
ccg upgrade       # pull the latest source and rebuild (see Upgrade below)
ccg version
```

### The review hub

After a commit is drafted (by AI, or after you fill the manual form), ccg shows a
**review hub**: the rendered commit message plus keybindings to jump straight to
any part — no need to walk through every field. From the hub:

| Key | Action |
|-----|--------|
| `enter` | Create the commit |
| `t` / `s` / `d` / `b` / `f` | Edit type / scope / description / body / footers |
| `!` | Toggle breaking change |
| `r` | Regenerate from the diff (only when AI is configured) |
| `e` | Edit everything (the full step-by-step form) |
| `q` / `esc` | Cancel |

### Keybindings (in the forms)

- **Select files:** `space` (or `x`) to toggle, `enter` to confirm.
- **Move between fields:** `enter` or `tab` (and `shift+tab` to go back). The form
  is paginated, so you see one short page at a time.
- **Multi-line body/footers:** `enter` advances to the next field; use
  `alt+enter` (or `ctrl+j`) to insert a newline within the body.
- **Cancel anytime:** `ctrl+c` (nothing is committed).

The **Footers** field takes one trailer per line, e.g. `Refs: #123`.

## Configuration

YAML, with precedence **env > project `.ccg.yaml` > global config > built-in defaults**.

- **Global:** `$XDG_CONFIG_HOME/ccg/config.yaml` (i.e. `~/.config/ccg/config.yaml` on WSL/Linux).
- **Project:** `.ccg.yaml` at the repository root (commit it to share team settings).

API keys are **never stored in config** — you configure the *name* of an
environment variable, and ccg reads the key from your environment at runtime.

### Example global config

```yaml
defaults: true            # include the built-in commit types
provider:
  base_url: https://api.openai.com/v1
  model: gpt-4o-mini
  api_key_env: OPENAI_API_KEY
  strict_schema: false    # opt into json_schema strict mode for providers that support it
commit:
  max_header_len: 72
```

### Example project `.ccg.yaml`

```yaml
provider:
  base_url: https://openrouter.ai/api/v1
  model: anthropic/claude-3.5-haiku
  api_key_env: OPENROUTER_API_KEY
commit:
  types:                  # custom types, merged with the defaults
    - name: infra
      description: Infrastructure / Terraform changes
```

### Environment overrides

`CCG_BASE_URL`, `CCG_MODEL`, `CCG_API_KEY_ENV`, `CCG_STRICT_SCHEMA` override the
corresponding config values for a single run.

## How AI generation works

ccg sends your **staged diff** (truncated to keep token cost down) plus your
optional hint and the allowed commit types to the model, asking for a single JSON
object describing the commit. The response is parsed defensively (code fences and
surrounding prose are tolerated); if parsing or the request fails, the workflow
degrades gracefully to manual editing rather than aborting. The draft always lands
in the editable review form before anything is committed.

## Development

### Prerequisites

- **Go 1.23+** (developed against 1.26). Check with `go version`.
- **git** on your `PATH` (the integration tests and the tool itself shell out to it).
- No CGO toolchain needed — `CGO_ENABLED=0` builds work everywhere.

### Project setup

```sh
git clone https://github.com/marvindinges/ccg.git
cd ccg
go mod download   # fetch dependencies (go build/test do this automatically too)
```

The module path is `github.com/marvindinges/ccg`. If you forked it under a different
namespace, update it once:

```sh
go mod edit -module github.com/<you>/ccg
grep -rl github.com/marvindinges/ccg --include='*.go' | xargs sed -i 's#github.com/marvindinges/ccg#github.com/<you>/ccg#g'
```

### Run locally

```sh
go run .                      # run without producing a binary
go run . --no-ai              # pass flags after the dot
go run . config               # run a subcommand
go run . --dry-run            # build the message and print it, don't commit
```

Run it inside a git repository with uncommitted changes so there's something to stage.

### Test

```sh
go test ./...                 # all unit + temp-repo integration tests
go test -count=1 ./...        # force a re-run (bypass the test cache)
go test -race ./...           # run with the race detector
go test -run TestRender ./internal/commit/   # a single test / package
go test -v ./internal/tui/    # verbose output
```

The `internal/git` tests build throwaway repositories in a temp dir and skip
automatically if `git` isn't on `PATH`. The TUI is tested by driving the Bubble
Tea model's `Update` with synthetic messages and injected fakes (no terminal
required).

### Test coverage

```sh
go test -cover ./...                          # per-package coverage summary
go test -coverprofile=cov.out ./...           # write a coverage profile
go tool cover -func=cov.out                   # per-function + total %
go tool cover -html=cov.out                   # open an annotated HTML report
go tool cover -html=cov.out -o coverage.html  # ...or write it to a file
```

### Lint / format

```sh
gofmt -l .        # list files needing formatting (empty = clean)
gofmt -w .        # format in place
go vet ./...      # static checks
```

### Build

```sh
go build -o ccg .                             # build for the current platform
go install github.com/marvindinges/ccg@latest       # install to $(go env GOPATH)/bin

# Cross-compile (CGO-free, so this just works):
GOOS=windows GOARCH=amd64 go build -o ccg.exe .
GOOS=linux   GOARCH=arm64 go build -o ccg-arm64 .

# Embed a version string (otherwise it reports "dev"):
go build -ldflags "-X github.com/marvindinges/ccg/internal/cmd.Version=$(git describe --tags --always)" -o ccg .
```

### Package layout

- `internal/commit` — Conventional Commits domain model (render/parse/validate); pure, no deps.
- `internal/git` — CGO-free `git` CLI wrapper (porcelain v2 status, stage, diff, commit, push).
- `internal/config` — YAML config loading and precedence.
- `internal/ai` — OpenAI-compatible client, prompt building, defensive JSON parsing.
- `internal/tui` — Bubble Tea model and the step-by-step flow.
- `internal/cmd` — cobra CLI wiring.
