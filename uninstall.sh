#!/bin/sh
# ccg uninstaller — removes everything the installer added, EXCEPT the Go
# toolchain (which ccg may have installed at ~/.local/go but other tools use too).
#
# It removes:
#   - the ccg binary            (default ~/.local/bin/ccg)
#   - the source clone          (default ~/.local/share/ccg)
#   - the global config         (~/.config/ccg)
#   - the PATH lines the installer added to your bash/zsh/fish rc files
#
# It asks ONE confirmation before doing anything. Run it the same way as install:
#   curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/uninstall.sh | sh
#
# Environment knobs (match the installer's, so a custom install can be reversed):
#   CCG_INSTALL_DIR   where the binary was installed (default ~/.local/bin)
#   CCG_SRC_DIR       where the source clone lives    (default ~/.local/share/ccg/src)
#   CCG_GO_DIR        the Go toolchain dir to KEEP    (default ~/.local/go)
#   CCG_ASSUME_YES=1  answer yes to the confirmation (non-interactive)
#   CCG_SKIP_PATH=1   do not touch shell rc files

set -eu

# ---------------------------------------------------------------------------
# Output helpers (mirrors install.sh)
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

tty_available() { (exec </dev/tty) 2>/dev/null; }

# ask <prompt> — yes/no, reads from the controlling terminal so it works under
# `curl | sh`. Returns 0 for yes, 1 for no.
ask() {
	_prompt="$1"
	if [ "${CCG_ASSUME_YES:-0}" = "1" ]; then
		return 0
	fi
	if ! tty_available; then
		return 1
	fi
	printf '%s [y/N] ' "$_prompt" >/dev/tty
	IFS= read -r _ans </dev/tty || _ans=""
	case "$_ans" in
	y | Y | yes | YES | Yes) return 0 ;;
	*) return 1 ;;
	esac
}

# ---------------------------------------------------------------------------
# Resolve the same paths the installer used
# ---------------------------------------------------------------------------
INSTALL_DIR="${CCG_INSTALL_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"
SRC_DIR="${CCG_SRC_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/ccg/src}"
GO_DIR="${CCG_GO_DIR:-$HOME/.local/go}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/ccg"

BIN="$INSTALL_DIR/ccg"
# The installer keeps the clone under <data>/ccg/src; remove the whole ccg dir.
DATA_DIR=$(dirname "$SRC_DIR")

# ---------------------------------------------------------------------------
# strip_path_lines <rcfile> — remove the "# Added by ccg installer" block (the
# comment, its export/fish_add_path lines, and a preceding blank line).
# ---------------------------------------------------------------------------
strip_path_lines() {
	_file="$1"
	[ -f "$_file" ] || return 0
	grep -qF "# Added by ccg installer" "$_file" 2>/dev/null || return 0

	_tmp="$_file.ccg.tmp"
	awk '
		$0 == "# Added by ccg installer" {
			if (held_set && held == "") { held_set = 0 }  # drop the leading blank
			skip = 1
			next
		}
		skip == 1 {
			if ($0 ~ /^export PATH=/ || $0 ~ /^fish_add_path /) next
			skip = 0
		}
		{
			if (held_set) print held
			held = $0; held_set = 1
		}
		END { if (held_set) print held }
	' "$_file" >"$_tmp" && mv "$_tmp" "$_file"
	info "  cleaned $_file"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
step "ccg uninstaller"
info ""
info "This will remove (the Go toolchain at ${c_bold}$GO_DIR${c_off} is kept):"
[ -e "$BIN" ]        && info "  • binary       $BIN"        || info "  ${c_dim}• binary       $BIN (not found)$c_off"
[ -d "$DATA_DIR" ]   && info "  • source clone $DATA_DIR"   || info "  ${c_dim}• source clone $DATA_DIR (not found)$c_off"
[ -d "$CONFIG_DIR" ] && info "  • config       $CONFIG_DIR" || info "  ${c_dim}• config       $CONFIG_DIR (not found)$c_off"
if [ "${CCG_SKIP_PATH:-0}" = "1" ]; then
	info "  ${c_dim}• PATH lines   (skipped: CCG_SKIP_PATH=1)$c_off"
else
	info "  • PATH lines   in your bash/zsh/fish rc files"
fi
info ""

if ! ask "Remove ccg and its config now?"; then
	info "Aborted. Nothing was removed."
	exit 0
fi

step "Removing ccg"
if [ -e "$BIN" ]; then
	rm -f "$BIN" && info "  removed $BIN"
fi
if [ -d "$DATA_DIR" ]; then
	rm -rf "$DATA_DIR" && info "  removed $DATA_DIR"
fi
if [ -d "$CONFIG_DIR" ]; then
	rm -rf "$CONFIG_DIR" && info "  removed $CONFIG_DIR"
fi

if [ "${CCG_SKIP_PATH:-0}" != "1" ]; then
	step "Cleaning PATH entries"
	strip_path_lines "$HOME/.bashrc"
	strip_path_lines "${ZDOTDIR:-$HOME}/.zshrc"
	strip_path_lines "$HOME/.config/fish/config.fish"
fi

info ""
step "Done — ccg removed."
if [ -d "$GO_DIR" ]; then
	info "Left the Go toolchain at $GO_DIR untouched. Remove it yourself if it was"
	info "installed only for ccg:  rm -rf $GO_DIR  (and its PATH line, if any)."
fi
info "Open a new shell (or re-source your rc file) for PATH changes to take effect."
