#!/usr/bin/env sh
# ccg installer — builds ccg from source and installs it into ~/.local/bin.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/install.sh | sh
#   # non-interactive (answer yes to everything):
#   curl -fsSL .../install.sh | CCG_ASSUME_YES=1 sh
#
# What it can do (each system change is announced and confirmed first):
#   - install the Go toolchain if it's missing or too old (no sudo, into ~/.local/go)
#   - build ccg from source and install it into ~/.local/bin
#   - add the relevant dirs to PATH in your bash/zsh/fish config
#   - optionally create a global config at ~/.config/ccg/config.yaml
#
# Environment knobs:
#   CCG_INSTALL_DIR          where to install the ccg binary (default ~/.local/bin)
#   CCG_SRC_DIR              where to keep the source clone (default ~/.local/share/ccg/src)
#   CCG_REPO_URL             git URL to clone (default the public GitHub repo)
#   CCG_REF                  git branch/tag/commit to build (default main)
#   CCG_GO_DIR               where to install Go if needed (default ~/.local/go)
#   CCG_SOURCE_DIR           build from this existing local checkout (skips cloning)
#   CCG_ASSUME_YES=1         answer yes to all prompts (non-interactive)
#   CCG_SKIP_PATH=1          do not modify shell rc files
#   CCG_SKIP_CONFIG=1        do not set up the global config
#   CCG_PROVIDER_BASE_URL    pre-fill provider base_url for config setup
#   CCG_PROVIDER_MODEL       pre-fill provider model
#   CCG_PROVIDER_API_KEY_ENV pre-fill the name of the API-key env var
#   CCG_PRIMARY_COLOR        pre-fill the primary accent color (default bright-blue)
#   CCG_SECONDARY_COLOR      pre-fill the secondary accent color (default bright-magenta)

set -eu

MODULE="github.com/marvindinges/ccg"
MIN_GO_MINOR=21 # minimum Go 1.x minor that can `go install` this module

# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	c_bold=$(printf '\033[1m'); c_dim=$(printf '\033[2m')
	c_red=$(printf '\033[31m'); c_yel=$(printf '\033[33m')
	c_mag=$(printf '\033[35m'); c_off=$(printf '\033[0m')
else
	c_bold=; c_dim=; c_red=; c_yel=; c_mag=; c_off=
fi

info() { printf '%s\n' "$*"; }
step() { printf '%s==>%s %s\n' "$c_mag$c_bold" "$c_off" "$*"; }
warn() { printf '%swarning:%s %s\n' "$c_yel" "$c_off" "$*" >&2; }
err() { printf '%serror:%s %s\n' "$c_red" "$c_off" "$*" >&2; }
die() { err "$*"; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

# tty_available — true only if /dev/tty can actually be opened. `[ -r /dev/tty ]`
# is unreliable: the device node can exist while no controlling terminal is
# attached (opening then fails with ENXIO), e.g. under some CI/pipe setups.
tty_available() { (exec </dev/tty) 2>/dev/null; }

# ask <prompt> — yes/no question. Reads from the controlling terminal so it works
# under `curl | sh` (where stdin is the script). Returns 0 for yes, 1 for no.
ask() {
	_prompt="$1"
	if [ "${CCG_ASSUME_YES:-0}" = "1" ]; then
		return 0
	fi
	if ! tty_available; then
		# No terminal and not assuming yes: treat as "no".
		return 1
	fi
	printf '%s [y/N] ' "$_prompt" >/dev/tty
	IFS= read -r _ans </dev/tty || _ans=""
	case "$_ans" in
	y | Y | yes | YES | Yes) return 0 ;;
	*) return 1 ;;
	esac
}

# prompt_value <prompt> <default> — read a free-text value from the terminal.
prompt_value() {
	_p="$1"; _default="${2:-}"
	if ! tty_available || [ "${CCG_ASSUME_YES:-0}" = "1" ]; then
		printf '%s' "$_default"; return 0
	fi
	if [ -n "$_default" ]; then
		printf '%s [%s]: ' "$_p" "$_default" >/dev/tty
	else
		printf '%s: ' "$_p" >/dev/tty
	fi
	IFS= read -r _v </dev/tty || _v=""
	[ -n "$_v" ] || _v="$_default"
	printf '%s' "$_v"
}

# ---------------------------------------------------------------------------
# Resolve settings
# ---------------------------------------------------------------------------
INSTALL_DIR="${CCG_INSTALL_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"
SRC_DIR="${CCG_SRC_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/ccg/src}"
REPO_URL="${CCG_REPO_URL:-https://github.com/marvindinges/ccg.git}"
REF="${CCG_REF:-main}"
GO_DIR="${CCG_GO_DIR:-$HOME/.local/go}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/ccg"
CONFIG_FILE="$CONFIG_DIR/config.yaml"

GO_BIN_TO_PATH="" # set if we install Go and need its bin on PATH

# ---------------------------------------------------------------------------
# OS / arch detection (for downloading Go)
# ---------------------------------------------------------------------------
detect_platform() {
	os=$(uname -s 2>/dev/null || echo unknown)
	arch=$(uname -m 2>/dev/null || echo unknown)
	case "$os" in
	Linux) GOOS=linux ;;
	Darwin) GOOS=darwin ;;
	*) GOOS="" ;;
	esac
	case "$arch" in
	x86_64 | amd64) GOARCH=amd64 ;;
	aarch64 | arm64) GOARCH=arm64 ;;
	*) GOARCH="" ;;
	esac
}

# ---------------------------------------------------------------------------
# Go detection & install
# ---------------------------------------------------------------------------
go_is_recent_enough() {
	have go || return 1
	# `go version` -> "go version go1.26.4 linux/amd64"
	_v=$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
	_major=$(printf '%s' "$_v" | cut -d. -f1)
	_minor=$(printf '%s' "$_v" | cut -d. -f2)
	[ "${_major:-0}" -gt 1 ] && return 0
	[ "${_major:-0}" -eq 1 ] && [ "${_minor:-0}" -ge "$MIN_GO_MINOR" ]
}

download() {
	# download <url> <dest>
	if have curl; then
		curl -fsSL "$1" -o "$2"
	elif have wget; then
		wget -qO "$2" "$1"
	else
		die "need 'curl' or 'wget' to download Go"
	fi
}

install_go() {
	detect_platform
	if [ -z "$GOOS" ] || [ -z "$GOARCH" ]; then
		die "unsupported platform for automatic Go install ($(uname -s)/$(uname -m)); install Go from https://go.dev/dl and re-run"
	fi

	step "Determining latest Go version…"
	have curl || have wget || die "need 'curl' or 'wget' to install Go"
	if have curl; then
		gover=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1)
	else
		gover=$(wget -qO- 'https://go.dev/VERSION?m=text' | head -n1)
	fi
	[ -n "$gover" ] || die "could not determine the latest Go version"

	tarball="${gover}.${GOOS}-${GOARCH}.tar.gz"
	url="https://go.dev/dl/${tarball}"
	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t ccg)
	trap 'rm -rf "$tmp"' EXIT

	step "Downloading ${gover} (${GOOS}/${GOARCH})…"
	download "$url" "$tmp/$tarball"

	step "Installing Go into $GO_DIR"
	# The tarball expands to a top-level 'go/' directory.
	parent=$(dirname "$GO_DIR")
	mkdir -p "$parent"
	rm -rf "$GO_DIR"
	tar -C "$parent" -xzf "$tmp/$tarball"
	# If GO_DIR isn't literally "<parent>/go", move it into place.
	if [ "$parent/go" != "$GO_DIR" ]; then
		rm -rf "$GO_DIR"
		mv "$parent/go" "$GO_DIR"
	fi
	rm -rf "$tmp"
	trap - EXIT

	PATH="$GO_DIR/bin:$PATH"
	export PATH
	GO_BIN_TO_PATH="$GO_DIR/bin"
	have go || die "Go install appears to have failed ($GO_DIR/bin/go not found)"
	info "Installed $(go version)"
}

ensure_go() {
	if go_is_recent_enough; then
		info "Using $(go version)"
		return 0
	fi
	if have go; then
		warn "Go $(go version 2>/dev/null | awk '{print $3}') is older than the required 1.$MIN_GO_MINOR."
	else
		info "Go is not installed."
	fi
	if ask "Install the latest Go toolchain into $GO_DIR (no sudo)?"; then
		install_go
	else
		die "Go 1.$MIN_GO_MINOR+ is required. Install it from https://go.dev/dl and re-run."
	fi
}

# ---------------------------------------------------------------------------
# Source clone + build
# ---------------------------------------------------------------------------
# clone_or_update fetches REF of REPO_URL into SRC_DIR (a persistent checkout
# that `ccg upgrade` reuses).
clone_or_update() {
	have git || die "git is required to download ccg. Install git and re-run, or build from an existing checkout with CCG_SOURCE_DIR=…"
	if [ -d "$SRC_DIR/.git" ]; then
		step "Updating source in $SRC_DIR ($REF)"
		# Keep the existing clone's origin; only override when explicitly asked,
		# so `ccg upgrade` reuses the URL the repo was cloned from.
		if [ -n "${CCG_REPO_URL:-}" ]; then
			git -C "$SRC_DIR" remote set-url origin "$REPO_URL" 2>/dev/null || true
		fi
		if git -C "$SRC_DIR" fetch --depth 1 --tags origin "$REF" 2>/dev/null; then
			git -C "$SRC_DIR" checkout -q -f FETCH_HEAD
		else
			git -C "$SRC_DIR" fetch --tags origin
			git -C "$SRC_DIR" checkout -q -f "$REF"
		fi
	else
		step "Cloning $REPO_URL → $SRC_DIR ($REF)"
		mkdir -p "$(dirname "$SRC_DIR")"
		rm -rf "$SRC_DIR"
		if ! git clone --depth 1 --branch "$REF" "$REPO_URL" "$SRC_DIR" 2>/dev/null; then
			# REF may be a commit (not valid for --branch): full clone + checkout.
			git clone "$REPO_URL" "$SRC_DIR"
			git -C "$SRC_DIR" checkout -q -f "$REF"
		fi
	fi
}

# ---------------------------------------------------------------------------
# Build & install ccg
# ---------------------------------------------------------------------------
install_ccg() {
	if [ -n "${CCG_SOURCE_DIR:-}" ]; then
		src="$CCG_SOURCE_DIR"
		step "Building ccg from $src"
	else
		clone_or_update
		src="$SRC_DIR"
	fi

	# Version string from the checkout (e.g. v0.1.0, or <tag>-<n>-g<sha>, or a sha).
	ver=$(git -C "$src" describe --tags --always --dirty 2>/dev/null || echo dev)
	[ -n "$ver" ] || ver=dev

	mkdir -p "$INSTALL_DIR"
	step "Building ccg ($ver) → $INSTALL_DIR/ccg"

	# Build to a temp file in the install dir (same filesystem → atomic rename,
	# and safe to replace the currently-running binary during `ccg upgrade`).
	# CGO is disabled: ccg is pure Go, and with cgo on, `-trimpath` rebuilds the
	# standard library through a C compiler — which fails on machines without a
	# working gcc/clang ("gcc: fatal error: no input files").
	tmpbin=$(mktemp "$INSTALL_DIR/.ccg.XXXXXX") || die "cannot write to $INSTALL_DIR"
	if ! (cd "$src" && CGO_ENABLED=0 go build -trimpath \
		-ldflags "-X ${MODULE}/internal/cmd.Version=${ver}" -o "$tmpbin" .); then
		rm -f "$tmpbin"
		die "build failed in $src"
	fi
	chmod +x "$tmpbin"
	mv "$tmpbin" "$INSTALL_DIR/ccg"
	[ -x "$INSTALL_DIR/ccg" ] || die "ccg was not produced in $INSTALL_DIR"
}

# ---------------------------------------------------------------------------
# PATH wiring
# ---------------------------------------------------------------------------
on_path() {
	case ":$PATH:" in
	*":$1:"*) return 0 ;;
	*) return 1 ;;
	esac
}

# append_line <file> <marker> <line> — append <line> once (idempotent via marker).
append_line() {
	_file="$1"; _marker="$2"; _line="$3"
	mkdir -p "$(dirname "$_file")"
	[ -f "$_file" ] || : >"$_file"
	if grep -qF "$_marker" "$_file" 2>/dev/null; then
		return 0
	fi
	printf '\n# Added by ccg installer\n%s\n' "$_line" >>"$_file"
	info "  updated $_file"
}

wire_path() {
	# Collect dirs that still need to be on PATH.
	dirs=""
	on_path "$INSTALL_DIR" || dirs="$INSTALL_DIR"
	if [ -n "$GO_BIN_TO_PATH" ] && ! on_path "$GO_BIN_TO_PATH"; then
		dirs="${dirs:+$dirs }$GO_BIN_TO_PATH"
	fi
	if [ -z "$dirs" ]; then
		info "PATH already contains the install dir(s); nothing to wire."
		return 0
	fi

	if [ "${CCG_SKIP_PATH:-0}" = "1" ]; then
		warn "Skipping PATH setup (CCG_SKIP_PATH=1). Add to PATH manually: $dirs"
		return 0
	fi
	if ! ask "Add to PATH ($dirs) in your shell config (bash/zsh/fish)?"; then
		warn "Not modifying shell config. Add these to PATH yourself: $dirs"
		return 0
	fi

	# POSIX export line for bash/zsh.
	_posix=""
	for d in $dirs; do _posix="export PATH=\"$d:\$PATH\"${_posix:+
$_posix}"; done

	# bash
	if [ -f "$HOME/.bashrc" ] || [ "${SHELL##*/}" = "bash" ]; then
		append_line "$HOME/.bashrc" "ccg installer" "$_posix"
	fi
	# zsh
	_zdot="${ZDOTDIR:-$HOME}"
	if [ -f "$_zdot/.zshrc" ] || [ "${SHELL##*/}" = "zsh" ]; then
		append_line "$_zdot/.zshrc" "ccg installer" "$_posix"
	fi
	# fish
	_fish="$HOME/.config/fish/config.fish"
	if [ -f "$_fish" ] || [ "${SHELL##*/}" = "fish" ]; then
		_fishline="fish_add_path $dirs"
		append_line "$_fish" "ccg installer" "$_fishline"
	fi
}

# ---------------------------------------------------------------------------
# Global config setup
# ---------------------------------------------------------------------------
setup_config() {
	if [ "${CCG_SKIP_CONFIG:-0}" = "1" ]; then
		return 0
	fi
	if ! ask "Set up a global config at $CONFIG_FILE now?"; then
		info "Skipping config setup. You can run 'ccg config' later or copy examples/ccg.yaml."
		return 0
	fi
	if [ -f "$CONFIG_FILE" ]; then
		if ! ask "$CONFIG_FILE already exists. Overwrite it?"; then
			info "Keeping existing config."
			return 0
		fi
	fi

	info ""
	info "Configure an AI provider (any OpenAI-compatible endpoint). Leave the base"
	info "URL blank to skip AI and use ccg in manual mode."
	base_url=$(prompt_value "Provider base URL (e.g. https://api.openai.com/v1)" "${CCG_PROVIDER_BASE_URL:-}")
	model=""
	key_env=""
	if [ -n "$base_url" ]; then
		model=$(prompt_value "Model (e.g. gpt-4o-mini)" "${CCG_PROVIDER_MODEL:-}")
		key_env=$(prompt_value "Name of the env var holding the API key (blank for local servers)" "${CCG_PROVIDER_API_KEY_ENV:-}")
	fi

	info ""
	info "Accent colors. Use a terminal color name (e.g. bright-blue, cyan, magenta)"
	info "to match your terminal theme, an ANSI 256 index (e.g. 141), or a #hex value."
	primary=$(prompt_value "Primary color" "${CCG_PRIMARY_COLOR:-bright-blue}")
	secondary=$(prompt_value "Secondary color" "${CCG_SECONDARY_COLOR:-bright-magenta}")

	mkdir -p "$CONFIG_DIR"
	{
		echo "defaults: true"
		if [ -n "$base_url" ]; then
			echo "provider:"
			echo "  base_url: $base_url"
			echo "  model: $model"
			if [ -n "$key_env" ]; then
				echo "  api_key_env: $key_env"
			fi
			echo "  strict_schema: false"
		fi
		echo "colors:"
		echo "  primary: $primary"
		echo "  secondary: $secondary"
		echo "commit:"
		echo "  max_header_len: 72"
	} >"$CONFIG_FILE"
	info "Wrote $CONFIG_FILE"
	if [ -n "$key_env" ]; then
		info "Remember to export your key, e.g.:  export $key_env=sk-…"
	fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
print_plan() {
	info "${c_bold}ccg installer${c_off}"
	info ""
	info "This will, asking before each system change:"
	if [ -n "${CCG_SOURCE_DIR:-}" ]; then
		info "  • build ccg from ${c_dim}$CCG_SOURCE_DIR${c_off}"
	else
		info "  • install Go if it's missing/older than 1.$MIN_GO_MINOR (into ${c_dim}$GO_DIR${c_off}, no sudo)"
		info "  • clone the source into ${c_dim}$SRC_DIR${c_off} and build it (ref ${c_dim}$REF${c_off})"
	fi
	info "  • install the ${c_bold}ccg${c_off} binary into ${c_dim}$INSTALL_DIR${c_off}"
	[ "${CCG_SKIP_PATH:-0}" = "1" ] || info "  • add it to your PATH (bash/zsh/fish)"
	[ "${CCG_SKIP_CONFIG:-0}" = "1" ] || info "  • optionally set up ${c_dim}$CONFIG_FILE${c_off}"
	info ""
}

main() {
	print_plan

	if [ "${CCG_ASSUME_YES:-0}" != "1" ] && ! tty_available; then
		err "No interactive terminal detected and CCG_ASSUME_YES is not set."
		err "Re-run in a terminal, or pass CCG_ASSUME_YES=1 to proceed non-interactively."
		exit 1
	fi
	if ! ask "Proceed?"; then
		info "Aborted. Nothing was changed."
		exit 0
	fi

	ensure_go
	install_ccg
	wire_path
	setup_config

	info ""
	step "Done."
	"$INSTALL_DIR/ccg" version || true
	info ""
	if ! on_path "$INSTALL_DIR"; then
		info "If 'ccg' isn't found, open a new shell or run:"
		info "  ${c_dim}export PATH=\"$INSTALL_DIR:\$PATH\"${c_off}"
	fi
	info "Next: run ${c_bold}ccg${c_off} in a repo, or ${c_bold}ccg config${c_off} to review settings."
}

main "$@"
