#!/usr/bin/env bash
# scripts/vendor.sh - download the pinned front-end assets and verify their checksums.
#
# The generated site must work offline, so marked and highlight.js are vendored into
# the binary instead of being loaded from a CDN. This script is the only supported
# way to refresh them: it pins exact versions and refuses to install a file whose
# SHA-256 does not match the value recorded in internal/site/shell/VENDOR.md.
#
#   scripts/vendor.sh            download, verify, install
#   scripts/vendor.sh --verify   verify the installed files only (no network)
#
# Adding a language: append a "name|url|sha256" line to LANGUAGES, run this script,
# then update the table in VENDOR.md.

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SHELL_DIR="${SCRIPT_DIR}/../internal/site/shell"
VENDOR_DIR="${SHELL_DIR}/vendor"

MARKED_VERSION="15.0.7"
HIGHLIGHT_VERSION="11.11.1"

MARKED_URL="https://cdn.jsdelivr.net/npm/marked@${MARKED_VERSION}/marked.min.js"
HIGHLIGHT_URL="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@${HIGHLIGHT_VERSION}/build/highlight.min.js"

# name|url|sha256 - the single source of truth for what gets embedded.
FILES=(
  "marked.min.js|${MARKED_URL}|934e3e36e9e2da0afb1a6e75075bb0f09af05293a844e84a7477ef40911c349a"
  "highlight.min.js|${HIGHLIGHT_URL}|c4a399dd6f488bc97a3546e3476747b3e714c99c57b9473154c6fb8d259b9381"
  "languages/go.min.js|https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@${HIGHLIGHT_VERSION}/build/languages/go.min.js|8d9f9a7dcd604438ac3530ad85597db9329923488b8a7ccc711df551f6f37799"
  "styles/github.min.css|https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@${HIGHLIGHT_VERSION}/build/styles/github.min.css|3a9a5def8b9c311e5ae43abde85c63133185eed4f0d9f67fea4b00a8308cf066"
  "styles/github-dark.min.css|https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@${HIGHLIGHT_VERSION}/build/styles/github-dark.min.css|9f208d022102b1d0c7aebfecd8e42ca7997d5de636649d2b31ea63093d809019"
)

# checksum_of prints the SHA-256 of a file in the same shape sha256sum uses.
checksum_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

verify_entry() {
  local relative="$1" expected="$2" target="${VENDOR_DIR}/$1"
  if [[ ! -f "${target}" ]]; then
    echo "missing: ${target}" >&2
    return 1
  fi
  local actual
  actual="$(checksum_of "${target}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "checksum mismatch for ${relative}" >&2
    echo "  expected ${expected}" >&2
    echo "  actual   ${actual}" >&2
    return 1
  fi
  echo "ok: ${relative}"
  return 0
}

verify_all() {
  local failures=0
  for entry in "${FILES[@]}"; do
    IFS='|' read -r relative _url expected <<<"${entry}"
    verify_entry "${relative}" "${expected}" || failures=$((failures + 1))
  done
  if ((failures > 0)); then
    echo "${failures} file(s) failed verification" >&2
    return 1
  fi
  echo "all vendored assets verified"
}

download_all() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' EXIT
  for entry in "${FILES[@]}"; do
    IFS='|' read -r relative url expected <<<"${entry}"
    echo "fetching ${relative}"
    curl -fsSL --retry 3 --retry-delay 2 -o "${tmp}/download" "${url}"
    local actual
    actual="$(checksum_of "${tmp}/download")"
    if [[ "${actual}" != "${expected}" ]]; then
      echo "checksum mismatch for ${relative}" >&2
      echo "  expected ${expected}" >&2
      echo "  actual   ${actual}" >&2
      echo "Refusing to install. If the upstream file legitimately changed, update the" >&2
      echo "checksum here and the table in internal/site/shell/VENDOR.md." >&2
      return 1
    fi
    mkdir -p "$(dirname "${VENDOR_DIR}/${relative}")"
    cp "${tmp}/download" "${VENDOR_DIR}/${relative}"
    echo "installed ${relative}"
  done
  echo "done - now rebuild: go build ./..."
}

main() {
  case "${1:-}" in
    --verify)
      verify_all
      ;;
    "")
      download_all
      verify_all
      ;;
    *)
      echo "usage: scripts/vendor.sh [--verify]" >&2
      exit 2
      ;;
  esac
}

main "$@"
