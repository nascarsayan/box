#!/bin/sh
# Install box, the rootless dev sandbox manager.
#
#   curl -fsSL https://snas.s.gy/box | sh -
#   curl -fsSL .../install.sh | sh -s -- -c ~/mybox -d /scratch/sandbox
#
# This script does one thing: put a single static binary on your PATH. Everything
# else -- nix, the configuration, the sandbox -- is `box init`, which it offers to
# run for you. No root, at any point.
#
# POSIX sh, because it is piped into whatever /bin/sh the host happens to have.
set -eu

REPO="${BOX_REPO:-nascarsayan/box}"
BIN_NAME="box"
INSTALL_DIR="${BOX_INSTALL_DIR:-}"
CONF_DIR=""
SANDBOX_DIR=""
RUN_INIT=1
VERSION="${BOX_VERSION:-latest}"
# Anything this script does not recognise is forwarded to `box init`, so the
# one-liner can drive the whole install: --yes, --no-rc, --modules ... .
INIT_ARGS=""

say()  { printf '\033[1;34m[box]\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33m[box]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[box]\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat >&2 <<'USAGE'
usage: install.sh [options]

  -n, --name NAME         install the binary under a different name (default: box)
  -c, --conf-dir DIR      where to clone the configuration (default ~/.config/box)
  -d, --sandbox-dir DIR   where nix and the sandbox live (default ~/sandbox)
      --install-dir DIR   where to put the binary (default ~/.local/bin)
      --version TAG       install a specific release (default: latest)
      --no-init           just install the binary, do not set anything up
  -h, --help              this

Any other option is passed straight to `box init`, so the whole install can be
driven from the one-liner:

  ... | sh -s -- --yes --no-rc
  ... | sh -s -- --modules kubernetes,ai
  ... | sh -s -- --apps kubectl,k9s,btop

Run `box help init` for that list. The binary is self-contained and static: no
nix, no root, no dependencies.
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    -n|--name)        BIN_NAME="${2:?--name needs a value}"; shift 2 ;;
    --name=*)         BIN_NAME="${1#*=}"; shift ;;
    -c|--conf-dir)    CONF_DIR="${2:?--conf-dir needs a path}"; shift 2 ;;
    --conf-dir=*)     CONF_DIR="${1#*=}"; shift ;;
    -d|--sandbox-dir) SANDBOX_DIR="${2:?--sandbox-dir needs a path}"; shift 2 ;;
    --sandbox-dir=*)  SANDBOX_DIR="${1#*=}"; shift ;;
    --install-dir)    INSTALL_DIR="${2:?--install-dir needs a path}"; shift 2 ;;
    --install-dir=*)  INSTALL_DIR="${1#*=}"; shift ;;
    --version)        VERSION="${2:?--version needs a tag}"; shift 2 ;;
    --version=*)      VERSION="${1#*=}"; shift ;;
    --no-init)        RUN_INIT=0; shift ;;
    -h|--help)        usage; exit 0 ;;
    --) shift; INIT_ARGS="$INIT_ARGS $*"; break ;;
    # Not ours: hand it to `box init` rather than rejecting it. The alternative is
    # an installer that cannot reach half of the thing it installs.
    *) INIT_ARGS="$INIT_ARGS $1"; shift ;;
  esac
done

command -v curl >/dev/null 2>&1 || die "curl is required"

case "$(uname -s)" in
  Linux) ;;
  *) die "box needs Linux: it uses user namespaces to provide /nix without root.
     On macOS install nix directly instead -- https://nixos.org/download" ;;
esac

case "$(uname -m)" in
  x86_64)        ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

# Default to a directory that is conventionally on PATH and needs no root.
if [ -z "$INSTALL_DIR" ]; then
  INSTALL_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
fi
mkdir -p "$INSTALL_DIR"

ASSET="box-linux-${ARCH}"
if [ "$VERSION" = latest ]; then
  URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
else
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
fi

TARGET="${INSTALL_DIR}/${BIN_NAME}"
TMP="${TARGET}.new.$$"

say "downloading ${ASSET} (${VERSION})"
if ! curl -fsSL "$URL" -o "$TMP"; then
  rm -f "$TMP"
  die "download failed: $URL
     If no release exists yet, build from source instead:
       git clone https://github.com/${REPO} && cd box && go build -o ${BIN_NAME} ."
fi
chmod 0755 "$TMP"
# Rename over the target so a half-finished download never leaves a broken binary,
# and so replacing a running box does not fail with ETXTBSY.
mv "$TMP" "$TARGET"
say "installed ${TARGET}"

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) warn "${INSTALL_DIR} is not on your PATH; add it to your shell rc:
       export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac

if [ "$RUN_INIT" -eq 0 ]; then
  say "run '${BIN_NAME} init' when you are ready"
  exit 0
fi

set --
[ -n "$CONF_DIR" ]    && set -- "$@" -c "$CONF_DIR"
[ -n "$SANDBOX_DIR" ] && set -- "$@" -d "$SANDBOX_DIR"
# Deliberately unquoted: INIT_ARGS is a flag list that must word-split.
# shellcheck disable=SC2086
set -- "$@" $INIT_ARGS

# stdin is the pipe this script arrived through, so the TUI needs the terminal
# handed to it explicitly or it would see EOF and fall back to defaults.
if [ -r /dev/tty ] && [ -t 1 ]; then
  exec "$TARGET" init "$@" < /dev/tty
fi
exec "$TARGET" init --yes "$@"
