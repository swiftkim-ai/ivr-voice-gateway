#!/bin/sh
# Drive a call that triggers an escalation through a real Asterisk and verify
# that the handoff packet reaches both the event log and the operator console.
#
# This is the Phase 3 integration test. Needs Docker.
#
#   ./deploy/asterisk/docker/run-escalation-test.sh
#
# What it proves: the caller presses 3, the agent escalates with a structured
# handoff packet, the event log contains the escalation message with the
# correct fields, the console WebSocket receives the handoff event, and
# Asterisk redirects the channel to the human agent's extension (300).

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"
image=ivrvg-asterisk:test
container=ivrvg-asterisk-escalation
workdir="$(mktemp -d)"
logdir="$workdir/calls"
console_log="$workdir/console-events.jsonl"

media_port=8090
ami_port=5038
ws_port=18099
secret=testsecret

cleanup() {
	[ -n "${tap_pid:-}" ] && kill "$tap_pid" 2>/dev/null || true
	[ -n "${gateway_pid:-}" ] && kill "$gateway_pid" 2>/dev/null || true
	docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

for port in "$media_port" "$ami_port" "$ws_port"; do
	if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
		echo "port $port is already in use; stop whatever holds it and retry:" >&2
		lsof -nP -iTCP:"$port" -sTCP:LISTEN >&2
		exit 1
	fi
done

echo "==> building the test switch"
docker build -q -t "$image" "$here" >/dev/null

echo "==> starting Asterisk"
docker rm -f "$container" >/dev/null 2>&1 || true
docker run -d --name "$container" \
	--add-host=host.docker.internal:host-gateway \
	-p "127.0.0.1:$ami_port:5038" \
	-v "$here/extensions.conf:/etc/asterisk/extensions.conf:ro" \
	-v "$here/manager.conf:/etc/asterisk/manager.conf:ro" \
	"$image" >/dev/null

echo "==> waiting for the dialplan"
i=0
until docker exec "$container" asterisk -rx "dialplan show 7000@ivrvg" 2>/dev/null | grep -q AudioSocket; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "Asterisk did not come up. Its log:" >&2
		docker logs "$container" >&2
		exit 1
	fi
	sleep 1
done
docker exec "$container" asterisk -rx "core show version" | head -1

echo "==> building the gateway and console tap"
mkdir -p "$logdir"
(cd "$repo" && go build -o "$workdir/ivrvg" ./cmd/ivrvg)
(cd "$repo" && go build -o "$workdir/console-tap" ./cmd/console-tap)

echo "==> starting the gateway"
IVRVG_AMI_SECRET="$secret" \
IVRVG_REDACTION_SALT=escalation-test-salt \
	"$workdir/ivrvg" serve \
	-addr "127.0.0.1:$ws_port" \
	-audiosocket ":$media_port" \
	-ami "127.0.0.1:$ami_port" \
	-ami-context default \
	-logdir "$logdir" >"$workdir/gateway.log" 2>&1 &
gateway_pid=$!

i=0
until grep -q "manager interface connected" "$workdir/gateway.log" 2>/dev/null; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "the gateway never reached the manager interface:" >&2
		cat "$workdir/gateway.log" >&2
		exit 1
	fi
	sleep 1
done

echo "==> connecting the console tap"
"$workdir/console-tap" \
	-url "ws://127.0.0.1:$ws_port/v1/console" \
	-out "$console_log" \
	-timeout 30s >"$workdir/console-tap.log" 2>&1 &
tap_pid=$!
sleep 1

echo "==> placing a call (caller will press 3 for billing escalation)"
docker exec "$container" asterisk -rx \
	"channel originate Local/7000@ivrvg/n extension s@ivrvg-caller-escalate" >/dev/null

# Wait for the call to complete.
i=0
until grep -q call_ended "$logdir"/*.jsonl 2>/dev/null; do
	i=$((i + 1))
	if [ "$i" -gt 40 ]; then
		echo "the call never finished. Gateway log:" >&2
		cat "$workdir/gateway.log" >&2
		echo "Asterisk log:" >&2
		docker logs "$container" 2>&1 | tail -30 >&2
		exit 1
	fi
	sleep 1
done

# Give the console tap a moment to receive the last events, then stop it.
sleep 2
kill "$tap_pid" 2>/dev/null || true
wait "$tap_pid" 2>/dev/null || true
tap_pid=""

call="$(ls "$logdir"/*.jsonl | head -1)"
echo "==> the call"
cat "$call"

echo ""
echo "==> console events"
cat "$console_log" 2>/dev/null || echo "(no console events)"

fail=0
check() {
	if [ "$2" = "true" ]; then
		echo "  ok   $1"
	else
		echo "  FAIL $1"
		fail=1
	fi
}

echo ""
echo "==> what the call proves"

# Basic call flow
check "the call opened" \
	"$(grep -q '"type":"call_started"' "$call" && echo true || echo false)"
check "the agent greeted the caller" \
	"$(grep -q '"type":"reply"' "$call" && echo true || echo false)"
check "DTMF 3 arrived" \
	"$(grep -q '"type":"dtmf"' "$call" && echo true || echo false)"

# Escalation in the event log
check "escalate action was emitted" \
	"$(grep -q '"kind":"escalate"' "$call" && echo true || echo false)"
check "escalation message in the log" \
	"$(grep -q '"type":"escalation"' "$call" && echo true || echo false)"

# Handoff fields in the escalation message
check "handoff has identity" \
	"$(grep '"type":"escalation"' "$call" | grep -q '"identity"' && echo true || echo false)"
check "handoff has intent" \
	"$(grep '"type":"escalation"' "$call" | grep -q '"intent"' && echo true || echo false)"
check "handoff has escalation_code BILLING" \
	"$(grep '"type":"escalation"' "$call" | grep -q '"escalation_code":"BILLING"' && echo true || echo false)"
check "handoff has summary" \
	"$(grep '"type":"escalation"' "$call" | grep -q '"summary"' && echo true || echo false)"
check "handoff has actions_taken" \
	"$(grep '"type":"escalation"' "$call" | grep -q '"actions_taken"' && echo true || echo false)"
check "handoff has sentiment" \
	"$(grep '"type":"escalation"' "$call" | grep -q '"sentiment":"calm"' && echo true || echo false)"

# The call ended
check "the call ended cleanly" \
	"$(grep -q '"type":"call_ended"' "$call" && echo true || echo false)"

# Console received the escalation
check "console received call_started" \
	"$(grep -q '"call_started"' "$console_log" 2>/dev/null && echo true || echo false)"
check "console received the escalation event" \
	"$(grep -q '"escalation"' "$console_log" 2>/dev/null && echo true || echo false)"
check "console handoff has BILLING code" \
	"$(grep '"escalation"' "$console_log" 2>/dev/null | grep -q '"escalation_code":"BILLING"' && echo true || echo false)"

# Switch-side redirect
if docker logs "$container" 2>&1 | grep -q "ESCALATION ARRIVED"; then
	echo "  ok   the escalation redirected the channel to extension 300"
else
	echo "  FAIL the switch never reached extension 300"
	fail=1
fi

# Replay
echo "==> replaying the recorded call"
if "$workdir/ivrvg" replay "$call"; then
	echo "  ok   the real call replays unchanged"
else
	echo "  FAIL the recorded call does not replay"
	fail=1
fi

if [ "$fail" -eq 0 ]; then
	echo "==> escalation integration test passed"
else
	echo "==> escalation integration test FAILED" >&2
fi
exit "$fail"
