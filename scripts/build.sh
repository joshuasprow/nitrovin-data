#!/usr/bin/env bash
# Build one data month from NHTSA's dump, as the release workflow does:
# fetch the dump, restore it into the Postgres oracle, extract it with
# nitrovin-import, and write the artifact and its manifest.
#
#   scripts/build.sh <nitrovin checkout> <month> <out dir>
#
# The nitrovin checkout supplies scripts/oracle.sh and cmd/nitrovin-import. It
# must be clean: manifest.json names its commit as the importer, and the
# release rebuilds with that commit. The oracle container ($ORACLE_CONTAINER,
# default nhtsa-oracle) must be running with an empty `vpic` database,
# collate C: `scripts/oracle.sh up` on a workstation, a postgres:17 service in
# CI.
#
# Writes to <out dir>:
#   vPICList_lite_<month>.custom.zip   NHTSA's dump, as downloaded
#   nitrovin-<month>.artifact          the artifact (format: pkg/artifact)
#   manifest.json                      the artifact's format, data month, size,
#                                      blake3 and rows per table; the dump's
#                                      URL, size and SHA-256; the oracle's
#                                      collation; the importer's commit
#
# Every field is a function of the dump and the importer, so a rebuild gives
# the same manifest, byte for byte.
set -euo pipefail

die() { echo "build: $*" >&2; exit 1; }

nitrovin="${1:?usage: scripts/build.sh <nitrovin checkout> <month> <out dir>}"
month="${2:?month}"
out="${3:?out dir}"
[[ "$month" =~ ^[0-9]{4}_[0-9]{2}$ ]] || die "month must be YYYY_MM, got '$month'"
container="${ORACLE_CONTAINER:-nhtsa-oracle}"
url="https://vpic.nhtsa.dot.gov/downloads/vPICList_lite_$month.custom.zip"

nitrovin=$(cd "$nitrovin" && pwd)
[ -z "$(git -C "$nitrovin" status --porcelain)" ] || die "$nitrovin has uncommitted changes: the manifest could not name the importer"
commit=$(git -C "$nitrovin" rev-parse HEAD)

mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fetched=$("$nitrovin/scripts/oracle.sh" fetch "$month" "$work")
field() { printf '%s\n' "$fetched" | sed -n "s/^$1=//p"; }
"$nitrovin/scripts/oracle.sh" restore "$(field backup)"

(cd "$nitrovin" && go build -o "$work/nitrovin-import" ./cmd/nitrovin-import)
"$work/nitrovin-import" sql |
  docker exec -i "$container" psql -X -q -v ON_ERROR_STOP=1 -U postgres -d vpic |
  "$work/nitrovin-import" build -month "$month" -o "$out/nitrovin-$month.artifact" -manifest "$work/artifact.json"

q() { docker exec "$container" psql -X -tA -U postgres -d vpic -c "$1"; }
jq \
  --arg url "$url" \
  --argjson bytes "$(field dump_bytes)" \
  --arg sha256 "$(field dump_sha256)" \
  --arg collate "$(q "select datcollate from pg_database where datname = 'vpic'")" \
  --arg ctype "$(q "select datctype from pg_database where datname = 'vpic'")" \
  --arg commit "$commit" \
  '. + {
    dump: {url: $url, bytes: $bytes, sha256: $sha256},
    collate: $collate,
    ctype: $ctype,
    importer: {module: "github.com/joshuasprow/nitrovin", package: "cmd/nitrovin-import", commit: $commit}
  }' "$work/artifact.json" >"$out/manifest.json"
mv "$work/vPICList_lite_$month.custom.zip" "$out/"
cat "$out/manifest.json"
