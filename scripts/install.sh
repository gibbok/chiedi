#!/usr/bin/env bash
set -euo pipefail
source_dir="${1:-$(cd "$(dirname "$0")/../bin" && pwd)}"
prefix="${PREFIX:-$HOME/.local}"
test -x "$source_dir/chiedi"
test -f "$source_dir/assets/manifest.json"
mkdir -p "$prefix"
prefix="$(cd "$prefix" && pwd -P)"
mkdir -p "$prefix/lib/chiedi/releases" "$prefix/bin"
# A running process must retain its executable and matching native assets.
# Keep previous releases; remove them only after their processes have stopped.
release="$(mktemp -d "$prefix/lib/chiedi/releases/install.XXXXXXXX")"
link_dir=""
installed=0
cleanup() {
  if [ -n "$link_dir" ]; then rm -rf "$link_dir"; fi
  if [ "$installed" -eq 0 ]; then rm -rf "$release"; fi
}
trap cleanup EXIT
cp "$source_dir/chiedi" "$release/chiedi"
cp -R "$source_dir/assets" "$release/assets"
link_dir="$(mktemp -d "$prefix/bin/.chiedi-link.XXXXXXXX")"
ln -s "$release/chiedi" "$link_dir/chiedi"
# Reject directories so mv cannot accidentally place the link inside one.
if [ -d "$prefix/bin/chiedi" ]; then
  printf 'Install destination is a directory: %s/bin/chiedi\n' "$prefix" >&2
  exit 1
fi
# Same filesystem: publish the complete release with one atomic rename.
mv -f "$link_dir/chiedi" "$prefix/bin/chiedi"
installed=1
printf 'Installed %s/bin/chiedi\nEnsure %s/bin is on PATH.\n' "$prefix" "$prefix"
