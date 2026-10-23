#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for tools/e2e/live_lib.sh: fail() caller prefix formatting and exit code,
# answers() against reachable and closed endpoints with bounded timeout, free_port()
# allocation and exhaustion bounds, qbit_password() retrieval, and add_image() parsing.
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
label="test-runner"
# shellcheck source=tools/e2e/live_lib.sh
. "$script_dir/live_lib.sh"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

expect() {
    local name=$1 want_status=$2 want_pattern=$3 got_status=$4 got_output=$5
    local status_ok=0
    if [ "$want_status" = "nonzero" ]; then
        [ "$got_status" -ne 0 ] && status_ok=1
    elif [ "$got_status" -eq "$want_status" ]; then
        status_ok=1
    fi

    if [ "$status_ok" -eq 1 ] && [[ "$got_output" =~ $want_pattern ]]; then
        echo "ok   $name"
    else
        echo "FAIL $name: status $got_status (want $want_status), output '$got_output' (pattern '$want_pattern')" >&2
        failed=1
    fi
}

# 1. fail() caller prefix, exit code, and boundary
out=$(label="custom-prefix" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    fail "expected error message"
' 2>&1) && status=0 || status=$?
expect "fail() includes caller prefix and exits 1" 1 '^custom-prefix: expected error message$' "$status" "$out"

out=$(label="custom-prefix" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    fail ""
' 2>&1) && status=0 || status=$?
expect "fail() handles empty message boundary" 1 '^custom-prefix: $' "$status" "$out"

out=$(unset label; bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    fail "should error on missing label"
' 2>&1) && status=0 || status=$?
expect "fail() requires label before sourcing" nonzero 'set label before sourcing live_lib.sh' "$status" "$out"

# 2. answers() against closed port, open port, and boundary
closed_port=$(free_port 26000)
out=$(label="answers-neg" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    answers "http://127.0.0.1:'"$closed_port"'" 1
' 2>&1) && status=0 || status=$?
expect "answers() against closed port fails at bound" 1 '^answers-neg: http://127.0.0.1:'"$closed_port"' did not answer within 1 s \(tries: 1\)$' "$status" "$out"

out=$(label="answers-env" ANSWERS_TIMEOUT=1 bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    answers "http://127.0.0.1:'"$closed_port"'"
' 2>&1) && status=0 || status=$?
expect "answers() respects ANSWERS_TIMEOUT override" 1 '^answers-env: http://127.0.0.1:'"$closed_port"' did not answer within 1 s \(tries: 1\)$' "$status" "$out"

# Positive case: reachable HTTP server
python3 -c "
import http.server, sys, os
sys.stderr = open(os.devnull, 'w')
server = http.server.HTTPServer(('127.0.0.1', 0), http.server.SimpleHTTPRequestHandler)
with open('$work/open_port', 'w') as f:
    f.write(str(server.server_port))
server.handle_request()
" &
py_http_pid=$!
for _ in $(seq 1 50); do
    [ -f "$work/open_port" ] && break
    sleep 0.1
done
open_port=$(cat "$work/open_port")
status=0
out=$(answers "http://127.0.0.1:$open_port" 5 2>&1) || status=$?
wait "$py_http_pid" 2>/dev/null || echo "live_lib_test: HTTP stub already gone" >&2
expect "answers() succeeds against open HTTP endpoint" 0 '^$' "$status" "$out"

out=$(label="answers-zero" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    answers "http://127.0.0.1:'"$closed_port"'" 0
' 2>&1) && status=0 || status=$?
expect "answers() with 0 bound fails immediately" 1 '^answers-zero: http://127.0.0.1:'"$closed_port"' did not answer within 0 s \(tries: 0\)$' "$status" "$out"

# 3. free_port() positive, exhaustion bound, and boundary
free_p=$(free_port 27000)
status=0
[[ "$free_p" =~ ^[0-9]+$ ]] && [ "$free_p" -ge 27000 ] || status=1
expect "free_port() finds an open port" 0 '.*' "$status" "$free_p"

python3 -c "
import socket, time
s = socket.socket()
s.bind(('127.0.0.1', 0))
s.listen(1)
with open('$work/bound_port', 'w') as f:
    f.write(str(s.getsockname()[1]))
time.sleep(5)
" &
py_sock_pid=$!
for _ in $(seq 1 50); do
    [ -f "$work/bound_port" ] && break
    sleep 0.1
done
bound_p=$(cat "$work/bound_port")
out=$(label="port-exhaust" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    free_port '"$bound_p"' 1
' 2>&1) && status=0 || status=$?
expect "free_port() fails when bound is exceeded" 1 '^port-exhaust: no free port from '"$bound_p"'$' "$status" "$out"

out=$(label="port-zero" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    free_port '"$bound_p"' 0
' 2>&1) && status=0 || status=$?
kill "$py_sock_pid" 2>/dev/null || echo "live_lib_test: socket stub already gone" >&2
wait "$py_sock_pid" 2>/dev/null || echo "live_lib_test: socket stub ended by the kill" >&2
expect "free_port() with max_tries=0 fails immediately" 1 '^port-zero: no free port from '"$bound_p"'$' "$status" "$out"

# 4. qbit_password() positive, timeout negative, and boundary
dc() {
    echo "2026-10-23 05:00:00.000000+00:00 [NOTICE] A temporary password is provided for this session: secretPass456"
}
status=0
pass=$(qbit_password 1) || status=$?
expect "qbit_password() extracts temporary password" 0 '^secretPass456$' "$status" "$pass"

# Mock dc with no password printed
out=$(label="qbit-neg" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    dc() { return 0; }
    qbit_password 1
' 2>&1) && status=0 || status=$?
expect "qbit_password() fails at timeout when no password printed" 1 '^qbit-neg: qBittorrent printed no temporary password within 1 s \(tries: 1\)$' "$status" "$out"

out=$(label="qbit-zero" bash -c '
    # shellcheck source=tools/e2e/live_lib.sh
    . "'"$script_dir"'/live_lib.sh"
    dc() { return 0; }
    qbit_password 0
' 2>&1) && status=0 || status=$?
expect "qbit_password() with 0 bound fails immediately" 1 '^qbit-zero: qBittorrent printed no temporary password within 0 s \(tries: 0\)$' "$status" "$out"

# 5. add_image() repository:tag positive, empty boundary, and malformed negatives
images=()
add_image api "ghcr.io/lusoris/snatcharr:pr-1"
add_image worker ""
status=0
[ "${images[*]}" = "--set image.api.repository=ghcr.io/lusoris/snatcharr --set image.api.tag=pr-1 --set image.api.pullPolicy=Never" ] || status=1
expect "add_image() splits repository:tag, skips an empty ref" 0 '.*' "$status" "${images[*]}"

for ref in "ghcr.io/lusoris/snatcharr" "localhost:5000/snatcharr" "snatcharr@sha256:abc"; do
    out=$(label="image-neg" bash -c '
        # shellcheck source=tools/e2e/live_lib.sh
        . "'"$script_dir"'/live_lib.sh"
        add_image api "'"$ref"'"
    ' 2>&1) && status=0 || status=$?
    expect "add_image() refuses $ref" 1 "^image-neg: api image $ref is not repository:tag\$" "$status" "$out"
done

exit "$failed"
