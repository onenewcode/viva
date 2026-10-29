#!/bin/sh
# Builds the release archives of the viva command.
#
#   tools/release/build.sh <version> <out-dir> <os/arch>...
#
# One archive per platform, viva_<version>_<os>_<arch>.tar.gz (.zip for Windows),
# holding the program and LICENSE. Builds are stripped and reproducible
# (-trimpath, no VCS stamp); the version is linked in, so `viva version` and
# `viva doctor` report the tag. Windows and Linux builds need no cgo. A macOS
# program reaches Cocoa through cgo, so darwin targets must be built on macOS
# (an amd64 build on an arm64 Mac works: the Xcode tools cross-compile).
#
# SHA256SUMS is written by tools/release/sums.sh once every archive is in place.
set -eu

if [ "$#" -lt 3 ]; then
  echo "usage: $0 <version> <out-dir> <os/arch>..." >&2
  exit 2
fi
version=$1
out=$2
shift 2
case "$version" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "build.sh: $version is not a version tag such as v0.1.3" >&2; exit 2 ;;
esac

root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for target in "$@"; do
  goos=${target%/*}
  goarch=${target#*/}
  name="viva_${version}_${goos}_${goarch}"
  stage="$work/$name"
  mkdir -p "$stage"
  exe=viva
  [ "$goos" = windows ] && exe=viva.exe
  cgo=0
  [ "$goos" = darwin ] && cgo=1
  echo "building $name" >&2
  (cd "$root" && CGO_ENABLED=$cgo GOOS=$goos GOARCH=$goarch go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$version" -o "$stage/$exe" ./cmd/viva)
  cp "$root/LICENSE" "$stage/LICENSE"
  # A fixed time, owner and order keep an archive the same for the same program.
  touch -t 200001010000 "$stage/$exe" "$stage/LICENSE"
  if [ "$goos" = windows ]; then
    (cd "$stage" && TZ=UTC zip -q -X "$out/$name.zip" "$exe" LICENSE)
  else
    if tar --version 2>/dev/null | grep -q GNU; then
      owner="--owner=0 --group=0 --numeric-owner"
    else
      owner="--uid 0 --gid 0 --numeric-owner"
    fi
    # shellcheck disable=SC2086
    (cd "$stage" && tar $owner -cf - "$exe" LICENSE | gzip -n -9 > "$out/$name.tar.gz")
  fi
done
