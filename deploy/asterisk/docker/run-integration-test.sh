#!/bin/sh
# Drive one real call through a real Asterisk and check what came out.
#
# This is the integration test the unit suite cannot be: it was written after a
# stand-in happily passed a call that real Asterisk hung up on. Needs Docker.
#
#   ./deploy/asterisk/docker/run-integration-test.sh
#
# What it proves: the dialplan loads, AudioSocket connects, DTMF arrives, a
# digit group is collected mid-call, the agent's transfer reaches the switch
# and moves a real channel, and the recorded call replays unchanged.

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"
image=ivrvg-asterisk:test
container=ivrvg-asterisk-test
workdir="$(mktemp -d)"
logdir="$workdir/calls"

# The gateway must be reachable from inside the container, and the manager
# interface from outside it.
media_port=8090
ami_port=5038
secret=testsecret

cleanup() {
	[ -n "${gateway_pid:-}" ] && kill "$gateway_pid" 2>/dev/null || true
	docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# A port already in use fails the call in a way that looks like a gateway bug,
# so say so plainly instead.
for port in "$media_port" "$ami_port"; do
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

# Wait for the switch rather than guessing how long it takes.
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

echo "==> starting the gateway"
mkdir -p "$logdir"
# Built rather than `go run`: go run spawns the compiled binary as a child, so
# killing it on cleanup leaves the server holding the port.
(cd "$repo" && go build -o "$workdir/ivrvg" ./cmd/ivrvg)
IVRVG_AMI_SECRET="$secret" \
IVRVG_REDACTION_SALT=integration-test-salt \
	"$workdir/ivrvg" serve \
	-addr 127.0.0.1:18099 \
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

echo "==> placing a call"
# A Local channel plays the caller, so no SIP phone is needed. /n keeps the two
# halves from being optimised away, which DTMF needs.
docker exec "$container" asterisk -rx \
	"channel originate Local/7000@ivrvg/n extension s@ivrvg-caller" >/dev/null

# The caller leg runs for about 11 seconds, then hangs up.
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

call="$(ls "$logdir"/*.jsonl | head -1)"
echo "==> the call"
cat "$call"

fail=0
check() {
	if grep -q "$2" "$call"; then
		echo "  ok   $1"
	else
		echo "  FAIL $1"
		fail=1
	fi
}

echo "==> what the call proves"
check "the call opened"                '"type":"call_started"'
check "the agent greeted the caller"   '"type":"reply"'
check "DTMF arrived over AudioSocket"  '"type":"dtmf"'
check "a digit group was collected"    '"type":"digits"'
check "the agent read the number back" '48217'
check "the agent transferred the call" '"kind":"transfer"'
check "the call ended cleanly"         '"type":"call_ended"'

if docker logs "$container" 2>&1 | grep -q "TRANSFER ARRIVED"; then
	echo "  ok   the transfer moved a real channel on the switch"
else
	echo "  FAIL the switch never reached extension 200"
	fail=1
fi

if docker logs "$container" 2>&1 | grep -q "Reached timeout after"; then
	echo "  FAIL Asterisk timed out the media stream"
	fail=1
else
	echo "  ok   the media stream was never starved"
fi

echo "==> replaying the recorded call"
if "$workdir/ivrvg" replay "$call"; then
	echo "  ok   the real call replays unchanged"
else
	echo "  FAIL the recorded call does not replay"
	fail=1
fi

if [ "$fail" -eq 0 ]; then
	echo "==> integration test passed"
else
	echo "==> integration test FAILED" >&2
fi
exit "$fail"
