#!/usr/bin/env bash
#
# Install the latest alpha release from GitHub.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/rapatel0/alpha/main/scripts/install.sh | bash
#
# Options (env):
#   ALPHA_VERSION      release tag to install (default: latest), e.g. v0.1.0
#   ALPHA_INSTALL_DIR  install directory (default: /usr/local/bin, else ~/.local/bin)
#   ALPHA_REPO         GitHub repo (default: rapatel0/alpha)
#   GITHUB_TOKEN     optional; raises API rate limits
#
# After install, appends BIN_DIR to common shell rc files (.zshenv, .bashrc,
# .profile, fish, and existing .zshrc/.zprofile/.bash_profile) so new terminals
# find `alpha` automatically. Prints a one-liner for the current session.

set -euo pipefail

REPO="${ALPHA_REPO:-rapatel0/alpha}"
API="https://api.github.com/repos/${REPO}"
DOWNLOAD_BASE="https://github.com/${REPO}/releases/download"

red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
info() { printf '%s\n' "$*" >&2; }

need_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		red "error: required command not found: $1"
		exit 1
	fi
}

need_cmd curl
need_cmd tar
need_cmd mktemp
need_cmd uname

# ---- platform ----
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
linux | darwin) ;;
mingw* | msys* | cygwin*)
	red "error: run scripts/install.ps1 in PowerShell for native Windows installs"
	red "  (or use the Windows .zip from GitHub Releases, or install via WSL)"
	exit 1
	;;
*)
	red "error: unsupported OS: $os"
	exit 1
	;;
esac
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*)
	red "error: unsupported CPU arch: $arch"
	exit 1
	;;
esac

# ---- install dir ----
if [ -n "${ALPHA_INSTALL_DIR:-}" ]; then
	BIN_DIR="$ALPHA_INSTALL_DIR"
elif [ -w /usr/local/bin ] 2>/dev/null || [ "$(id -u)" -eq 0 ]; then
	BIN_DIR="/usr/local/bin"
elif mkdir -p "${HOME}/.local/bin" 2>/dev/null && [ -w "${HOME}/.local/bin" ]; then
	BIN_DIR="${HOME}/.local/bin"
else
	red "error: no writable install directory (set ALPHA_INSTALL_DIR)"
	exit 1
fi
mkdir -p "$BIN_DIR"

# ---- curl helper ----
# Avoid empty-array expansion under `set -u` (breaks on macOS Bash 3.2).
github_curl() {
	if [ -n "${GITHUB_TOKEN:-}" ]; then
		curl -fsSL -H "Authorization: Bearer ${GITHUB_TOKEN}" "$@"
	else
		curl -fsSL "$@"
	fi
}

# ---- resolve version ----
if [ -n "${ALPHA_VERSION:-}" ]; then
	TAG="$ALPHA_VERSION"
	case "$TAG" in
	v*) ;;
	*) TAG="v${TAG}" ;;
	esac
else
	info "alpha install: querying latest release..."
	json="$(github_curl \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		"${API}/releases/latest")" || {
		red "error: failed to query ${API}/releases/latest"
		red "hint: publish a release first, or set ALPHA_VERSION=vX.Y.Z"
		exit 1
	}
	TAG="$(printf '%s' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
	if [ -z "$TAG" ]; then
		red "error: could not parse tag_name from GitHub API response"
		exit 1
	fi
fi

# GoReleaser .Version strips the leading v from the tag.
VERSION="${TAG#v}"
ASSET="alpha_${VERSION}_${os}_${arch}.tar.gz"
SUMS="checksums_${VERSION}.txt"
ASSET_URL="${DOWNLOAD_BASE}/${TAG}/${ASSET}"
SUMS_URL="${DOWNLOAD_BASE}/${TAG}/${SUMS}"

info "alpha install: ${TAG} (${os}/${arch})"
info "alpha install: ${ASSET_URL}"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/alpha-install.XXXXXX")"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

# ---- download ----
info "alpha install: downloading checksums..."
github_curl -o "${TMP}/${SUMS}" "$SUMS_URL"

info "alpha install: downloading archive..."
github_curl -o "${TMP}/${ASSET}" "$ASSET_URL"

# ---- verify ----
info "alpha install: verifying checksum..."
want="$(awk -v f="$ASSET" '$2 == f { print $1; exit }' "${TMP}/${SUMS}")"
if [ -z "$want" ]; then
	red "error: checksum for ${ASSET} not found in ${SUMS}"
	exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "${TMP}/${ASSET}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
	got="$(shasum -a 256 "${TMP}/${ASSET}" | awk '{print $1}')"
else
	red "error: need sha256sum or shasum to verify the download"
	exit 1
fi

if [ "$(printf '%s' "$got" | tr '[:upper:]' '[:lower:]')" != "$(printf '%s' "$want" | tr '[:upper:]' '[:lower:]')" ]; then
	red "error: checksum mismatch for ${ASSET}"
	red "  want: ${want}"
	red "  got:  ${got}"
	exit 1
fi

# ---- extract + install ----
info "alpha install: extracting..."
tar -xzf "${TMP}/${ASSET}" -C "$TMP"
if [ ! -f "${TMP}/alpha" ]; then
	red "error: archive did not contain a alpha binary"
	exit 1
fi
chmod 755 "${TMP}/alpha"

DEST="${BIN_DIR}/alpha"
info "alpha install: installing to ${DEST}"
# Prefer atomic replace when possible.
if mv -f "${TMP}/alpha" "$DEST" 2>/dev/null; then
	:
else
	# /usr/local/bin may need sudo
	if command -v sudo >/dev/null 2>&1; then
		sudo mv -f "${TMP}/alpha" "$DEST"
		sudo chmod 755 "$DEST"
	else
		red "error: cannot write to ${DEST} (try ALPHA_INSTALL_DIR=\$HOME/.local/bin)"
		exit 1
	fi
fi

info "alpha install: installed ${TAG} -> ${DEST}"

# ---- PATH configuration (mirrors jcode installer: multi-rc, idempotent) ----
# A child script cannot mutate the parent shell's PATH. We persist to rc files
# so future terminals find alpha; print a one-liner for THIS terminal.
case ":${PATH}:" in
*":${BIN_DIR}:"*) on_path=1 ;;
*) on_path=0 ;;
esac

# System dirs like /usr/local/bin are usually already on PATH — skip rc writes.
if [ "$BIN_DIR" != "/usr/local/bin" ] || [ "$on_path" -eq 0 ]; then
	PATH_LINE="export PATH=\"${BIN_DIR}:\$PATH\""
	added_to=""

	_have() { command -v "$1" >/dev/null 2>&1; }

	# ensure_posix_rc <rc-file> <create:yes|no>
	# create=no only patches existing files (never create ~/.bash_profile — that
	# would stop bash from reading ~/.profile on login).
	ensure_posix_rc() {
		rc="$1"
		create="$2"
		if [ ! -f "$rc" ]; then
			[ "$create" = "yes" ] || return 0
			mkdir -p "$(dirname "$rc")"
		fi
		if ! grep -qF "$BIN_DIR" "$rc" 2>/dev/null; then
			printf '\n# Added by alpha installer\n%s\n' "$PATH_LINE" >>"$rc"
			added_to="$added_to $rc"
		fi
	}

	ensure_fish_rc() {
		create="$1"
		rc="${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
		if [ ! -f "$rc" ]; then
			[ "$create" = "yes" ] || return 0
			mkdir -p "$(dirname "$rc")"
		fi
		if ! grep -qF "$BIN_DIR" "$rc" 2>/dev/null; then
			{
				printf '\n# Added by alpha installer\n'
				printf 'if not contains "%s" $PATH\n' "$BIN_DIR"
				printf '    set -gx PATH "%s" $PATH\n' "$BIN_DIR"
				printf 'end\n'
			} >>"$rc"
			added_to="$added_to $rc"
		fi
	}

	# zsh: ~/.zshenv is read for every zsh invocation.
	if _have zsh || [ "$(uname -s)" = "Darwin" ] || [ -f "$HOME/.zshenv" ] || [ -f "$HOME/.zshrc" ]; then
		ensure_posix_rc "$HOME/.zshenv" yes
	fi

	# bash: ~/.bashrc (interactive) + ~/.profile (login). Never create ~/.bash_profile.
	if _have bash || [ -f "$HOME/.bashrc" ] || [ -f "$HOME/.bash_profile" ]; then
		ensure_posix_rc "$HOME/.bashrc" yes
	fi
	ensure_posix_rc "$HOME/.profile" yes

	if _have fish || [ -f "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish" ]; then
		ensure_fish_rc yes
	fi

	# Patch other existing startup files without creating them.
	for rc in "$HOME/.zshrc" "$HOME/.zprofile" "$HOME/.bash_profile"; do
		ensure_posix_rc "$rc" no
	done

	if [ -n "$added_to" ]; then
		info "alpha install: added ${BIN_DIR} to PATH in:${added_to}"
	fi
fi

info ""
info "alpha install: ${TAG} installed successfully!"
info ""

resolved="$(command -v alpha 2>/dev/null || true)"
if [ -n "$resolved" ] && [ "$resolved" -ef "$DEST" ]; then
	info "Run 'alpha config' to get started."
elif [ -n "$resolved" ] && [ ! "$resolved" -ef "$DEST" ]; then
	red "alpha install: warning: 'alpha' still resolves to a different binary:"
	red "  ${resolved}"
	red "  (new binary is at ${DEST})"
	info ""
	info "  To use the new binary in THIS terminal right now, run:"
	info ""
	printf '    \033[1;32mexport PATH="%s:$PATH" && alpha config\033[0m\n' "$BIN_DIR" >&2
	info ""
	info "  Or remove the old binary:  rm -f \"${resolved}\""
	info "  Future terminal sessions will prefer ${BIN_DIR}."
else
	info "  To start using alpha in THIS terminal right now, run:"
	info ""
	printf '    \033[1;32mexport PATH="%s:$PATH" && alpha config\033[0m\n' "$BIN_DIR" >&2
	info ""
	info "  Future terminal sessions will have alpha on PATH automatically."
fi

info ""
info "Next steps:"
info "  1. alpha config          # add a model + api_key (opens in browser)"
info "  2. alpha                 # start the TUI"
info ""
info "Or set ALPHA_MODEL and ALPHA_API_KEY, then run 'alpha'."
