# ccg

An interactive TUI for writing [Conventional Commits](https://www.conventionalcommits.org/),
with **optional** AI generation.
written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea) v2 and
[huh](https://github.com/charmbracelet/huh).

- **Panel workflow:** a lazygit-style layout — a **Files** panel (stage individual
  files or whole folders) beside a **Commit** review panel you edit via popup
  modals — with optional AI drafting and an optional push at the end.
- **You always verify.** AI output is never committed without you reviewing and
  editing it in the Commit panel first.
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
`CCG_SKIP_CONFIG=1`. The installer can pre-fill **every** config value, too:
`CCG_PROVIDER_BASE_URL`, `CCG_PROVIDER_MODEL`, `CCG_PROVIDER_API_KEY_ENV`,
`CCG_STRICT_SCHEMA`, `CCG_PRIMARY_COLOR`, `CCG_SECONDARY_COLOR`,
`CCG_MAX_HEADER_LEN`, `CCG_COUNTDOWN_SECONDS`, `CCG_DEFAULTS`, and `CCG_TYPES`.


### Upgrade

```sh
ccg upgrade            # pull the latest source and rebuild
ccg upgrade v0.2.0     # build a specific tag/branch/commit
```

`ccg upgrade` reuses the source checkout from the installer (`~/.local/share/ccg/src`),
rebuilds, and replaces the current binary. Re-running the install script does the
same thing. (If ccg was installed another way, re-run the install command above.)

### Uninstall

```sh
ccg uninstall          # remove ccg (asks once first)
ccg uninstall -y       # remove without the confirmation prompt
```

`ccg uninstall` runs the uninstaller from the source checkout: it removes the
binary, the source checkout (`~/.local/share/ccg`), and the global config
(`~/.config/ccg`). It **keeps the Go toolchain** and leaves your shell rc files
untouched (remove the `# Added by ccg installer` PATH line yourself if you want).
If ccg was installed another way, run the uninstaller directly:

```sh
curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/uninstall.sh | sh
```

### Manual install

Requires Go 1.21+ (the toolchain auto-fetches the version pinned in `go.mod`) and `git`.

```sh
git clone https://github.com/marvindinges/ccg.git && cd ccg
CGO_ENABLED=0 go build -o ccg .
# move ./ccg onto your PATH, e.g. into ~/.local/bin
```

(ccg is pure Go; `CGO_ENABLED=0` avoids needing a C compiler.)

## Usage

Run inside a git repository:

```sh
ccg
```

Flags:

| Flag | Description |
|------|-------------|
| `--no-ai` | Force manual mode even if a provider is configured |
| `--hint "..."` | Pre-fill the natural-language AI hint (the hint prompt opens pre-filled) |
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
ccg uninstall     # remove ccg, its source checkout, and config (see Uninstall below)
ccg version
```

### The interface

ccg opens a two-panel layout. Press `tab` to switch panels; the focused panel
expands and the other collapses to a one-line summary.

**Files panel** — choose what to commit. Files are grouped into a folder tree, so
you can stage a single file or a whole folder at once. Staging is applied to git
immediately and is always reversible.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Move the cursor |
| `space` | Stage / unstage the file or folder under the cursor |
| `a` | Stage / unstage everything |
| `enter` | Proceed: open the AI hint prompt (with AI) or jump to the Commit panel |

**Commit panel** — the rendered commit message, edited with a keypress per part.
The header line is colour-coded by length: green up to 50 characters, yellow past
50, red once it exceeds the configured `max_header_len` (default 72); a `len/max`
counter is shown next to it.

| Key | Action |
|-----|--------|
| `t` / `s` / `d` / `b` / `f` | Edit type / scope / description / body / footers |
| `!` | Toggle breaking change |
| `r` | (Re)generate from the diff via a hint prompt (only when AI is configured) |
| `e` | Edit everything in one form |
| `y` | Copy the commit message to the clipboard |
| `c` / `enter` | Commit (asks whether to push, then counts down) |

Editing any field opens a **popup modal**: type the value, `enter` to submit,
`esc` to cancel. Multi-line body/footers take `alt+enter` (or `ctrl+j`) for a
newline within the field. The **Footers** field takes one trailer per line, e.g.
`Refs: #123`.

**Commit/push are abortable.** Pressing `c` (or `enter`) first asks whether to
push (unless `--push`/`--no-push` is set), then runs a countdown (default 3s, set
`countdown_seconds`; `0` runs immediately) before committing and pushing. Press
`esc` during the countdown to cancel the whole thing — nothing is committed or
pushed. Clipboard copy (`y`) uses `wl-copy`, `xclip`, `xsel`, `pbcopy`, or
`clip.exe`, whichever is available.

**SSH passphrase prompt.** If the first push attempt fails because your SSH key
needs a passphrase (and no agent has it loaded), ccg shows a masked input field
so you can type the passphrase directly in the TUI instead of the push hanging
indefinitely. Press `esc` on the passphrase prompt to skip the push while keeping
the commit.

Global keys everywhere: `tab` switch panel · `q` quit · `ctrl+c` abort (nothing
is committed).

## Configuration

YAML, with precedence **env > project `.ccg.yaml` > global config > built-in defaults**.

- **Global:** `$XDG_CONFIG_HOME/ccg/config.yaml` (i.e. `~/.config/ccg/config.yaml` on WSL/Linux).
- **Project:** `.ccg.yaml` at the repository root (commit it to share team settings).

API keys are **never stored in config** — you configure the *name* of an
environment variable, and ccg reads the key from your environment at runtime.

### Example global config

```yaml
defaults: true            # include the built-in commit types
countdown_seconds: 3      # abortable delay before commit/push (0 = no countdown)
no_push: false            # set true to always skip the push step (same as --no-push)
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

### Colors

The TUI's two accent colors are configurable. By default they use **terminal
palette colors** (`bright-blue` / `bright-magenta`), so ccg matches your terminal
theme. Set them in the global config:

```yaml
colors:
  primary: bright-blue       # badges, borders, spinner tail
  secondary: bright-magenta  # keybinding keys, selectors, spinner head
```

Each value is a terminal color name (`black`, `red`, `green`, `yellow`, `blue`,
`magenta`, `cyan`, `white`, and the `bright-*` variants), an ANSI 256 index
(e.g. `141`), or a hex value (e.g. `#a06bff`). The installer can set these for you.

### Environment overrides

Every config option has a `CCG_*` environment variable that overrides it for a
single run (precedence: env > project > global > defaults):

| Env var | Overrides |
|---------|-----------|
| `CCG_BASE_URL` | `provider.base_url` |
| `CCG_MODEL` | `provider.model` |
| `CCG_API_KEY_ENV` | `provider.api_key_env` |
| `CCG_STRICT_SCHEMA` | `provider.strict_schema` (bool) |
| `CCG_PRIMARY_COLOR` | `colors.primary` |
| `CCG_SECONDARY_COLOR` | `colors.secondary` |
| `CCG_MAX_HEADER_LEN` | `commit.max_header_len` (int) |
| `CCG_COUNTDOWN_SECONDS` | `countdown_seconds` (int) |
| `CCG_DEFAULTS` | `defaults` (bool) |
| `CCG_NO_PUSH` | `no_push` (bool) |
| `CCG_TYPES` | `commit.types`, as `"name:desc;name:desc"` |

`ccg config` prints each resolved value and where it came from (`default`,
`global`, `project`, or `env`).

## How AI generation works

ccg sends your **staged diff** (truncated to keep token cost down) plus your
optional hint, the current **branch name** (for extra context, e.g. a ticket id),
and the allowed commit types to the model, asking for a single JSON
object describing the commit. The response is parsed defensively (code fences and
surrounding prose are tolerated); if parsing or the request fails, the workflow
degrades gracefully to manual editing rather than aborting. The draft always lands
in the editable Commit panel before anything is committed.

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

