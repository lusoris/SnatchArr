#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Proves the write path of rule id_matched (ADR-0013) on real Sonarr and Radarr
# (tools/e2e/forceimport/compose.yaml), never on a live stack. Each app grabs one release by
# ID under a title it cannot parse, so the queue item blocks with the ID-match note; the
# Sonarr torrent also holds an unmapped file. TestLiveForceImport
# (api/internal/arrclient/forceimport_live_test.go) then reads the queue, sweeps in act mode,
# sends the ManualImport through the production client and reads the queue back. The run
# must pass and must not skip, and each library must hold exactly the one imported file.
#
# SNATCHARR_FORCEIMPORT_PLANT plants a case the rule must refuse:
#   audio        the videos carry no audio track, so the *arr rejects every candidate
#   unmapped     the Sonarr torrent holds two unmapped files and no episode file
#   second-line  the test adds a second problem to each item's status lines
#   skip         the test never sends the import, so the read-back finds the item queued
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
plant=${SNATCHARR_FORCEIMPORT_PLANT:-}
project="snatcharr-forceimport-$$"
work=$(mktemp -d)
dl="$work/dl"
sonarr="http://127.0.0.1:${SONARR_PORT:-18201}"
radarr="http://127.0.0.1:${RADARR_PORT:-18202}"
qbit="http://127.0.0.1:${QBITTORRENT_PORT:-18203}"
FORCEIMPORT_DOWNLOADS=$dl
FORCEIMPORT_UID=$(id -u)
FORCEIMPORT_GID=$(id -g)
export FORCEIMPORT_DOWNLOADS FORCEIMPORT_UID FORCEIMPORT_GID

case "$plant" in
'' | audio | unmapped | second-line | skip) ;;
*) echo "forceimport-live: unknown SNATCHARR_FORCEIMPORT_PLANT '$plant'" >&2; exit 1 ;;
esac

# shellcheck source=tools/e2e/pull_retry.sh
. "$root/tools/e2e/pull_retry.sh"
dc() { compose -p "$project" -f "$root/tools/e2e/forceimport/compose.yaml" "$@"; }

cleanup() {
    status=$?
    if [ "$status" -ne 0 ] && ! dc logs --no-color --tail 30 sonarr radarr qbittorrent; then
        echo "forceimport-live: could not read the app logs" >&2
    fi
    if ! dc down -v --remove-orphans >/dev/null 2>&1; then
        echo "forceimport-live: could not remove project $project" >&2
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

label="forceimport-live"
# shellcheck source=tools/e2e/live_lib.sh
. "$(dirname "$0")/live_lib.sh"

# api_key SERVICE: the key the *arr wrote into its config.xml on first start.
api_key() {
    local i key
    for i in $(seq 1 60); do
        key=$(dc exec -T "$1" sed -n 's:.*<ApiKey>\(.*\)</ApiKey>.*:\1:p' /config/config.xml 2>/dev/null | tr -d '\r')
        if [ -n "$key" ]; then printf '%s' "$key"; return 0; fi
        sleep 1
    done
    fail "$1 wrote no API key within 60 s (tries: $i)"
}

# video PATH: a 25-minute black video, long enough for the *arr's sample check, with a
# silent audio track unless plant=audio.
video() {
    local audio=(-f lavfi -i anullsrc=r=8000:cl=mono -c:a aac -b:a 8k)
    if [ "$plant" = audio ]; then audio=(); fi
    dc run --rm -T --quiet-pull --user "$FORCEIMPORT_UID:$FORCEIMPORT_GID" --entrypoint ffmpeg ffmpeg \
        -loglevel error -f lavfi -i color=c=black:s=64x36:r=1 "${audio[@]}" -t 1500 \
        -c:v libx264 -preset ultrafast "/downloads/$1" || fail "ffmpeg could not write $1"
}

# media: one torrent per app under dl/torrents, its info hash in WORK/<app>.hash.
media() {
    local show="Totally Unrelated Show S01E02 1080p WEB-DL x264-SNATCH"
    local movie="Ganz Anderer Film 1999 1080p WEB-DL x264-SNATCH"
    mkdir -p "$dl/library/tv" "$dl/library/movies" "$dl/torrents" "$dl/$show" "$dl/$movie"
    video "$movie/$movie.mkv"
    # Sonarr parses S09E99 to an episode the series lacks: the candidate is rejected. It
    # takes the grab-history mapping only when it is the download's one file, so the unmapped
    # plant puts a second unmappable file in its place.
    cp "$dl/$movie/$movie.mkv" "$dl/$show/Bonus Feature S09E99.mkv"
    if [ "$plant" = unmapped ]; then
        cp "$dl/$movie/$movie.mkv" "$dl/$show/Bonus Feature S09E98.mkv"
    else
        cp "$dl/$movie/$movie.mkv" "$dl/$show/$show.mkv"
    fi
    python3 -I "$root/tools/e2e/forceimport/mktorrent.py" "$dl/$show" "$dl/torrents/sonarr.torrent" >"$work/sonarr.hash"
    python3 -I "$root/tools/e2e/forceimport/mktorrent.py" "$dl/$movie" "$dl/torrents/radarr.torrent" >"$work/radarr.hash"
}

# library DIR: the imported files; exactly one, so the unmapped file stayed behind.
library() {
    local n
    n=$(find "$dl/library/$1" -type f -name '*.mkv' | wc -l)
    [ "$n" -eq 1 ] || fail "library/$1 holds $n files, want 1: $(find "$dl/library/$1" -type f)"
    echo "ok   imported $(find "$dl/library/$1" -type f -name '*.mkv' -printf '%P\n')"
}

pull_retry dc --profile tools pull --quiet
dc up -d --pull never sonarr radarr qbittorrent files
for url in "$sonarr" "$radarr" "$qbit"; do
    answers "$url"
done
(
    umask 077
    api_key sonarr >"$work/sonarr.key"
    api_key radarr >"$work/radarr.key"
    qbit_password >"$work/qbit.pass"
)
media
python3 -I "$root/tools/e2e/forceimport/arr_setup.py" "$work" "$plant"

if ! out=$(cd "$root" && SNATCHARR_FORCEIMPORT_LIVE="$work/live.json" \
    go test -count=1 -run '^TestLiveForceImport$' -v ./api/internal/arrclient/ 2>&1); then
    printf '%s\n' "$out" >&2
    fail "TestLiveForceImport failed"
fi
case "$out" in
*"--- SKIP"*) printf '%s\n' "$out" >&2; fail "TestLiveForceImport skipped" ;;
*"--- PASS: TestLiveForceImport "*) ;;
*) printf '%s\n' "$out" >&2; fail "TestLiveForceImport did not run" ;;
esac
library tv
library movies
echo "ok   Sonarr and Radarr force-imported the ID-matched download, the unmapped file left behind"
