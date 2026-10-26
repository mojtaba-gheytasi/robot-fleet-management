#!/usr/bin/env bash
# Runs PRD workflows A–E against the running service and checks every answer.
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:${HTTP_PORT:-8080}}"
API="$BASE_URL/api"
ALERT_WAIT="${ALERT_WAIT:-6}"
RUN="demo-$(date +%s)-$$"

BODY=""
STATUS=""

step() { printf '\n== %s\n' "$*"; }
say() { printf '   %s\n' "$*"; }

fail() {
	printf '\nFAILED: %s\n   response (%s): %s\n' "$1" "$STATUS" "$BODY" >&2
	exit 1
}

request() {
	local method=$1 path=$2 data=${3:-} response
	if [ -n "$data" ]; then
		response=$(curl -sS -X "$method" -H 'Content-Type: application/json' -d "$data" -w $'\n%{http_code}' "$API$path")
	else
		response=$(curl -sS -X "$method" -w $'\n%{http_code}' "$API$path")
	fi
	BODY=${response%$'\n'*}
	STATUS=${response##*$'\n'}
}

expect() {
	local want_status=$1
	shift
	[ "$STATUS" = "$want_status" ] || fail "expected HTTP $want_status"
	for want in "$@"; do
		case "$BODY" in *"$want"*) ;; *) fail "expected the response to contain $want" ;; esac
	done
}

expect_not() {
	case "$BODY" in *"$1"*) fail "expected the response not to contain $1" ;; esac
}

iso() {
	local epoch=$(($(date +%s) + $1))
	date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -r "$epoch" +%Y-%m-%dT%H:%M:%SZ
}

register() {
	request POST /robots "{\"id\":\"$1\",\"site_id\":\"site-north\"}"
	expect 201 '"lifecycle":"ACTIVE"' '"battery_health":"UNKNOWN"' '"latest_observation":null'
	say "registered $1"
}

send() {
	local robot=$1 id=$2 observed_at=$3 battery=$4 mode=${5:-WORKING}
	request POST "/robots/$robot/telemetry" \
		"{\"observation_id\":\"$id\",\"observed_at\":\"$observed_at\",\"battery_percent\":$battery,\"position\":{\"x\":12.5,\"y\":24.8},\"operating_mode\":\"$mode\"}"
}

alert_id_of() {
	printf '%s' "$BODY" | grep -o "\"id\":\"[^\"]*\",\"robot_id\":\"$1\"" | head -1 | cut -d'"' -f4 || true
}

alert_count_of() {
	{ printf '%s' "$BODY" | grep -o "\"robot_id\":\"$1\"" || true; } | wc -l | tr -d ' '
}

alert_entry() {
	printf '%s' "$BODY" | grep -o "\"id\":\"$1\"[^}]*" || true
}

wait_for_alert_check() {
	say "waiting ${ALERT_WAIT}s for the alert check"
	sleep "$ALERT_WAIT"
}

curl -sSf "$BASE_URL/readyz" >/dev/null 2>&1 || {
	echo "The service is not reachable at $BASE_URL. Start it with 'make up' (set HTTP_PORT if it is not 8080)." >&2
	exit 1
}

step "A — Register and monitor"
A="$RUN-a"
register "$A"
send "$A" a-1 "$(iso -1)" 80
expect 200 '"outcome":"applied"'
A_REPORTED=$(date +%s)
request GET "/robots/$A"
expect 200 '"connectivity":"ONLINE"' '"battery_health":"NORMAL"' '"battery_percent":80'
say "first snapshot applied: 80%, NORMAL, ONLINE"

step "B — Handle low battery"
B="$RUN-b"
register "$B"
send "$B" b-1 "$(iso -3)" 18
expect 200 '"outcome":"applied"'
say "sent 18%"
wait_for_alert_check
request GET /alerts
expect 200
B_ALERT=$(alert_id_of "$B")
[ -n "$B_ALERT" ] || fail "expected an open alert for $B"
say "alert $B_ALERT is OPEN"
request POST "/alerts/$B_ALERT/acknowledge"
expect 200 '"status":"ACKNOWLEDGED"'
say "acknowledged"
send "$B" b-2 "$(iso -2)" 15
expect 200 '"outcome":"applied"'
say "sent 15%"
wait_for_alert_check
request GET /alerts
[ "$(alert_count_of "$B")" = "1" ] || fail "expected still exactly one unresolved alert for $B"
say "still one alert, not a second one"
send "$B" b-3 "$(iso -1)" 30
expect 200 '"outcome":"applied"'
say "sent 30%"
wait_for_alert_check
request GET "/alerts?state=terminal"
entry=$(alert_entry "$B_ALERT")
case "$entry" in *'"status":"RESOLVED"'*) ;; *) fail "expected alert $B_ALERT to be RESOLVED" ;; esac
case "$entry" in *'"acknowledged_at":null'*) fail "expected the acknowledgement to be kept" ;; esac
say "alert RESOLVED, acknowledgement kept"

step "C — Buffered delivery"
C="$RUN-c"
register "$C"
LATE=$(iso -2)
EARLY=$(iso -1)
send "$C" c-B "$EARLY" 70
expect 200 '"outcome":"applied"'
say "snapshot B (newer) arrived first: applied"
send "$C" c-A "$LATE" 71
expect 200 '"outcome":"superseded"'
say "snapshot A (older) arrived late: superseded"
request GET "/robots/$C"
expect 200 '"observation_id":"c-B"'
say "current state is still B"
send "$C" c-A "$LATE" 71
expect 200 '"outcome":"duplicate"'
send "$C" c-B "$EARLY" 70
expect 200 '"outcome":"duplicate"'
say "resending either one: duplicate"
request GET "/robots/$C/telemetry"
expect 200 '"observation_id":"c-B"' '"observation_id":"c-A"'
case "$(printf '%s' "$BODY" | grep -o '"observation_id":"c-A"[^}]*}[^}]*')" in *'"outcome":"SUPERSEDED"'*) ;; *) fail "expected c-A to be SUPERSEDED in history" ;; esac
say "history keeps both, A marked SUPERSEDED"

step "D — Robot stops reporting"
elapsed=$(($(date +%s) - A_REPORTED))
if [ "$elapsed" -le 11 ]; then
	say "waiting $((11 - elapsed))s without a snapshot from $A"
	sleep $((11 - elapsed))
fi
request GET "/robots/$A"
expect 200 '"connectivity":"STALE"' '"battery_percent":80'
say "$A is STALE and still shows its last reading"

step "E — Deactivate and reactivate"
E="$RUN-e"
register "$E"
E_LOW=$(iso -1)
send "$E" e-1 "$E_LOW" 15
expect 200 '"outcome":"applied"'
say "sent 15%"
wait_for_alert_check
request GET /alerts
E_ALERT=$(alert_id_of "$E")
[ -n "$E_ALERT" ] || fail "expected an open alert for $E"
say "alert $E_ALERT is OPEN"
request POST "/robots/$E/deactivate"
expect 200 '"lifecycle":"INACTIVE"' '"battery_health":"LOW"' '"battery_percent":15'
request GET "/alerts?state=terminal"
entry=$(alert_entry "$E_ALERT")
case "$entry" in *'"status":"CLOSED"'*'"closure_reason":"ROBOT_DEACTIVATED"'*) ;; *) fail "expected alert $E_ALERT to be CLOSED by the deactivation" ;; esac
say "deactivated: alert CLOSED (ROBOT_DEACTIVATED) at once, last reading kept"
send "$E" e-2 "$(iso 0)" 14
expect 409 '"code":"ROBOT_INACTIVE"'
say "a new snapshot is rejected: ROBOT_INACTIVE"
send "$E" e-1 "$E_LOW" 15
expect 200 '"outcome":"duplicate"'
say "a resend is still a duplicate"
request POST "/robots/$E/activate"
expect 200 '"lifecycle":"ACTIVE"' '"battery_health":"LOW"'
say "reactivated, still LOW"
wait_for_alert_check
request GET /alerts
[ "$(alert_count_of "$E")" = "0" ] || fail "expected no alert before a fresh snapshot"
say "no alert yet: no snapshot since reactivation"
send "$E" e-3 "$(iso 0)" 18
expect 200 '"outcome":"applied"'
say "sent a fresh 18%"
wait_for_alert_check
request GET /alerts
[ "$(alert_count_of "$E")" = "1" ] || fail "expected a new alert after the fresh low snapshot"
say "a new alert opened"

step "Fleet summary"
request GET /fleet/summary
expect 200 '"registered"'
say "$BODY"

printf '\nAll workflows passed.\n'
