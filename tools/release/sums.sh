#!/bin/sh
# Writes SHA256SUMS for the archives in a directory, in the format sha256sum
# prints and tools/releasepkg reads.
#
#   tools/release/sums.sh <dir>
set -eu
[ "$#" -eq 1 ] || { echo "usage: $0 <dir>" >&2; exit 2; }
cd "$1"
files=$(ls viva_v*.tar.gz viva_v*.zip 2>/dev/null | LC_ALL=C sort)
[ -n "$files" ] || { echo "sums.sh: no archives in $1" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  # shellcheck disable=SC2086
  sha256sum $files > SHA256SUMS
else
  # shellcheck disable=SC2086
  shasum -a 256 $files > SHA256SUMS
fi
cat SHA256SUMS
