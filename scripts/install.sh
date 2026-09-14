#!/usr/bin/env bash
set -euo pipefail
source_dir="${1:-$(cd "$(dirname "$0")/../bin" && pwd)}"
prefix="${PREFIX:-$HOME/.local}"
test -x "$source_dir/chiedi"
test -f "$source_dir/assets/manifest.json"
mkdir -p "$prefix/lib/chiedi" "$prefix/bin"
cp "$source_dir/chiedi" "$prefix/lib/chiedi/chiedi"
# Copy the complete directory, including model/runtime license notices.
cp -R "$source_dir/assets" "$prefix/lib/chiedi/"
ln -sfn "$prefix/lib/chiedi/chiedi" "$prefix/bin/chiedi"
printf 'Installed %s/bin/chiedi\nEnsure %s/bin is on PATH.\n' "$prefix" "$prefix"
