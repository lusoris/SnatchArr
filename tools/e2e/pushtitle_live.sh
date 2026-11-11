#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# image-pin-check: record (the Sonarr and qBittorrent pairs verified for ADR-0019 open
# point 1; no updater moves them, so tools/ci/image_pin_check.py compares them only with
# the other record files)
#
# Proves Sonarr v3 and v4 push-title import guarantee (ADR-0019 open point 1)
# on real Sonarr and qBittorrent, never on a live stack. Pushing a release
# candidate rewritten to the canonical series title ensures grab history
# records SeriesMatchType.Title, guaranteeing automatic import of downloads
# with localized directory and file names.
#
# SNATCHARR_PUSHTITLE_SONARR selects the Sonarr/qBittorrent release pair:
#   v4 (default)  Sonarr 4.0.20 + qBittorrent 5.3.0 (tools/e2e/forceimport/compose.yaml)
#   v3            Sonarr 3.0.10 + qBittorrent 4.6.2 (tools/e2e/pushtitle/images.yaml)
#
# SNATCHARR_PUSHTITLE_PLANT plants a defect the check must refuse:
#   foreign  s1 pushes the foreign folder title instead of the canonical title -> s1 not imported
#   idonly   s3 pushes an unparseable title with TVDB ID -> grab matched by ID, import blocked
#
# SNATCHARR_PUSHTITLE_EVIDENCE_DIR destination directory for evidence JSON files:
#   $SNATCHARR_PUSHTITLE_EVIDENCE_DIR/$sonarr_ver-${plant:-clean}.json
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
sonarr_ver=${1:-${SNATCHARR_PUSHTITLE_SONARR:-v4}}
plant=${SNATCHARR_PUSHTITLE_PLANT:-}
project="snatcharr-pushtitle-$sonarr_ver-$$"
work=$(mktemp -d)
dl="$work/dl"

export SONARR_PORT=${SONARR_PORT:-18231}
export RADARR_PORT=${RADARR_PORT:-18232}
export QBITTORRENT_PORT=${QBITTORRENT_PORT:-18233}
sonarr="http://127.0.0.1:$SONARR_PORT"
qbit="http://127.0.0.1:$QBITTORRENT_PORT"

FORCEIMPORT_DOWNLOADS=$dl
FORCEIMPORT_UID=$(id -u)
FORCEIMPORT_GID=$(id -g)
export FORCEIMPORT_DOWNLOADS FORCEIMPORT_UID FORCEIMPORT_GID

case "$sonarr_ver" in
v3 | v4) ;;
*) echo "pushtitle-live: unknown SNATCHARR_PUSHTITLE_SONARR '$sonarr_ver'" >&2; exit 1 ;;
esac

case "$plant" in
'' | foreign | idonly) ;;
*) echo "pushtitle-live: unknown SNATCHARR_PUSHTITLE_PLANT '$plant'" >&2; exit 1 ;;
esac

case "$sonarr_ver" in
v3)
    SNATCHARR_PUSHTITLE_SONARR_IMAGE="lscr.io/linuxserver/sonarr:3.0.10.1567-ls218@sha256:4f69472a0cdea29e23967db6a3395487539b759def3920327d1f49ae00b9dfd4"
    SNATCHARR_PUSHTITLE_QBIT_IMAGE="docker.io/linuxserver/qbittorrent:4.6.2@sha256:ef49e9a76faa73c5a594e2dbd89b5a7a3503abe53bcde5277f999f869658cdf6"
    ;;
v4)
    SNATCHARR_PUSHTITLE_SONARR_IMAGE="lscr.io/linuxserver/sonarr:4.0.20.3014-ls329@sha256:44b06045a98bc1f237b9ffefc4b28dab64cb29305e4a2a94875ae93b5224e4cc"
    SNATCHARR_PUSHTITLE_QBIT_IMAGE="docker.io/linuxserver/qbittorrent:5.3.0@sha256:e887804fa9de4c60f5f0fb1f3a7e4febe40a8f69fe736f70c4e8589b1f868dab"
    ;;
esac
export SNATCHARR_PUSHTITLE_SONARR_IMAGE SNATCHARR_PUSHTITLE_QBIT_IMAGE
export SNATCHARR_PUSHTITLE_SONARR="$sonarr_ver"
export SNATCHARR_PUSHTITLE_PLANT="$plant"

# shellcheck source=tools/e2e/pull_retry.sh
. "$root/tools/e2e/pull_retry.sh"
override="$root/tools/e2e/pushtitle/images.yaml"

# dc: compose wrapper targeting the forceimport stack with pushtitle image overrides.
dc() { compose -p "$project" -f "$root/tools/e2e/forceimport/compose.yaml" -f "$override" "$@"; }

# cleanup: stop containers, dump logs on failure, and delete temporary working directory.
cleanup() {
    status=$?
    if [ "$status" -ne 0 ] && ! dc logs --no-color --tail 30 sonarr qbittorrent; then
        echo "pushtitle-live: could not read the app logs" >&2
    fi
    if ! dc down -v --remove-orphans >/dev/null 2>&1; then
        echo "pushtitle-live: could not remove project $project" >&2
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

label="pushtitle-live"
# shellcheck source=tools/e2e/live_lib.sh
. "$root/tools/e2e/live_lib.sh"

# api_key SERVICE: retrieve the generated API key from Sonarr config.xml.
api_key() {
    local i key
    for i in $(seq 1 90); do
        key=$(dc exec -T "$1" sed -n 's:.*<ApiKey>\(.*\)</ApiKey>.*:\1:p' /config/config.xml 2>/dev/null | tr -d '\r')
        if [ -n "$key" ]; then printf '%s' "$key"; return 0; fi
        sleep 1
    done
    fail "$1 wrote no API key within 90 s (tries: $i)"
}

# video PATH: create a dummy test video using the tools profile ffmpeg container.
video() {
    dc run --rm -T --quiet-pull --user "$FORCEIMPORT_UID:$FORCEIMPORT_GID" --entrypoint ffmpeg ffmpeg \
        -loglevel error -f lavfi -i color=c=black:s=64x36:r=1 -f lavfi -i anullsrc=r=8000:cl=mono \
        -c:a aac -b:a 8k -t 1500 -c:v libx264 -preset ultrafast "/downloads/$1" || fail "ffmpeg could not write $1"
}

# mk ID FOLDER FILE: create sample media file, directory structure, and torrent file with info hash.
mk() {
    local id=$1 folder=$2 file=$3 top
    if [ -n "$folder" ]; then
        mkdir -p "$dl/$folder"
        cp "$dl/src/base.mkv" "$dl/$folder/$file"
        top=${folder%%/*}
        python3 -I "$root/tools/e2e/forceimport/mktorrent.py" "$dl/$top" "$dl/torrents/$id.torrent" >"$work/$id.hash"
    else
        cp "$dl/src/base.mkv" "$dl/$file"
        python3 -I "$root/tools/e2e/forceimport/mktorrent.py" "$dl/$file" "$dl/torrents/$id.torrent" >"$work/$id.hash"
    fi
}

# media: set up test library and create the 7 media cases and torrent files.
media() {
    mkdir -p "$dl/library/tv" "$dl/torrents" "$dl/src"
    video "src/base.mkv"
    local f="Alone Ueberlebe die Wildnis"
    local c="GERMAN 1080p WEB-DL x264-SNATCH"
    mk s1 "$f S01E01 $c" "$f S01E01 $c.mkv"
    mk s2 "" "$f S01E03 $c.mkv"
    mk s3 "$f S01E04 $c" "$f S01E04 $c.mkv"
    mk s4 "$f S01E05 $c" "$f S01E05 $c.mkv"
    mk s5 "Kaefer S01E06 $c" "kfr-106.mkv"
    mk s6 "$f S01E07 $c" "$f S01E07 $c.mkv"
    mk s7 "$f Staffel 1 $c/Disc 1/$f S01E02 $c" "$f S01E02 $c.mkv"
}

pull_retry dc --profile tools pull --quiet
dc up -d --pull never sonarr qbittorrent files
for url in "$sonarr" "$qbit"; do
    answers "$url"
done
(
    umask 077
    api_key sonarr >"$work/sonarr.key"
    qbit_password >"$work/qbit.pass"
)
media
if [ -n "${SNATCHARR_PUSHTITLE_EVIDENCE:-}" ]; then
    evidence=$SNATCHARR_PUSHTITLE_EVIDENCE
elif [ -n "${SNATCHARR_PUSHTITLE_EVIDENCE_DIR:-}" ]; then
    mkdir -p "$SNATCHARR_PUSHTITLE_EVIDENCE_DIR"
    evidence="$SNATCHARR_PUSHTITLE_EVIDENCE_DIR/$sonarr_ver-${plant:-clean}.json"
else
    evidence="$work/evidence.json"
fi
python3 -I "$root/tools/e2e/pushtitle/check.py" "$work" "$evidence"
echo "ok   Sonarr $sonarr_ver push-title import guarantee verified"
