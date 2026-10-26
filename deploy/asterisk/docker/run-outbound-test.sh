#!/bin/sh
# Outbound integration test: map a stub IVR, verify the flow map, prove diff.
#
# Mirrors run-integration-test.sh but for the Phase 2 outbound path. Needs
# Docker.
#
#   ./deploy/asterisk/docker/run-outbound-test.sh
#
# What it proves: `ivrvg map` originates calls through AMI, Asterisk creates
# the Local channels, AudioSocket connects back to the gateway, the scenario
# runs (step/action events are logged), a flow map is produced from the logs,
# and `ivrvg diff` detects a change between two maps.

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"
image=ivrvg-asterisk:test
container=ivrvg-asterisk-outbound
workdir="$(mktemp -d)"
logdir="$workdir/outbound"
mapfile="$workdir/map.json"

media_port=8090
ami_port=5038
secret=testsecret

cleanup() {
	[ -n "${gateway_pid:-}" ] && kill "$gateway_pid" 2>/dev/null || true
	docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

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
	-p "127.0.0.1:$ami_port:5038" \
	-v "$here/extensions.conf:/etc/asterisk/extensions.conf:ro" \
	-v "$here/manager.conf:/etc/asterisk/manager.conf:ro" \
	"$image" >/dev/null

echo "==> waiting for the dialplan"
i=0
until docker exec "$container" asterisk -rx "dialplan show s@stub-ivr" 2>/dev/null | grep -q WaitExten; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "Asterisk did not come up. Its log:" >&2
		docker logs "$container" >&2
		exit 1
	fi
	sleep 1
done
docker exec "$container" asterisk -rx "core show version" | head -1

echo "==> building ivrvg"
mkdir -p "$logdir"
(cd "$repo" && go build -o "$workdir/ivrvg" ./cmd/ivrvg)

echo "==> running ivrvg map against the stub IVR"
# ivrvg map originates the calls, opens AudioSocket, runs each scenario, then
# builds a flow map from the event logs. The -channel template uses a Local
# channel so no SIP trunk is needed.
IVRVG_AMI_SECRET="$secret" \
	"$workdir/ivrvg" map \
	-ami "127.0.0.1:$ami_port" \
	-channel 'Local/{number}/n' \
	-audiosocket ":$media_port" \
	-scenario "$repo/testdata/scenarios/stub-ivr.json" \
	-out "$mapfile" \
	-logdir "$logdir" \
	-accept-timeout 15s \
	>"$workdir/map.log" 2>&1 || {
	echo "ivrvg map failed:" >&2
	cat "$workdir/map.log" >&2
	echo "Asterisk log:" >&2
	docker logs "$container" 2>&1 | tail -30 >&2
	exit 1
}

cat "$workdir/map.log"

fail=0
check() {
	if [ "$2" = "true" ]; then
		echo "  ok   $1"
	else
		echo "  FAIL $1"
		fail=1
	fi
}

echo "==> verifying event logs"
log_count=$(ls "$logdir"/*.jsonl 2>/dev/null | wc -l | tr -d ' ')
check "at least two call logs written" "$([ "$log_count" -ge 2 ] && echo true || echo false)"

# Each log should have call_started, at least one step, and call_ended.
for logfile in "$logdir"/*.jsonl; do
	base=$(basename "$logfile")
	check "$base has call_started" "$(grep -q call_started "$logfile" && echo true || echo false)"
	check "$base has step events" "$(grep -q '"type":"step"' "$logfile" && echo true || echo false)"
	check "$base has call_ended" "$(grep -q call_ended "$logfile" && echo true || echo false)"
done

echo "==> verifying the flow map"
echo "--- map contents ---"
cat "$mapfile"
echo "--- end map ---"

# The flow map should exist and contain nodes and edges.
check "map file exists" "$([ -f "$mapfile" ] && echo true || echo false)"
check "map has nodes" "$(grep -q '"nodes"' "$mapfile" && echo true || echo false)"
check "map has edges" "$(grep -q '"edges"' "$mapfile" && echo true || echo false)"

# Expect the greeting node (shared by both scenarios).
check "greeting node present" "$(grep -q '"greeting"' "$mapfile" && echo true || echo false)"
# Expect the sales and support nodes.
check "sales node present" "$(grep -q '"sales"' "$mapfile" && echo true || echo false)"
check "support node present" "$(grep -q '"support"' "$mapfile" && echo true || echo false)"

# Expect edges from greeting via digits 1 and 2.
check "edge greeting->sales:1" "$(grep -q '"from":"greeting"' "$mapfile" && grep -q '"input":"1"' "$mapfile" && echo true || echo false)"
check "edge greeting->support:2" "$(grep -q '"from":"greeting"' "$mapfile" && grep -q '"input":"2"' "$mapfile" && echo true || echo false)"

echo "==> verifying ivrvg diff"
# Identical maps should produce no diff (exit 0).
if "$workdir/ivrvg" diff "$mapfile" "$mapfile" >/dev/null 2>&1; then
	echo "  ok   diff of identical maps exits 0"
else
	echo "  FAIL diff of identical maps should exit 0"
	fail=1
fi

# Create a modified map (remove the support node) to seed a regression.
modified="$workdir/map-modified.json"
# Remove the support node and its edge to simulate a regression.
sed 's/"support"/"support_v2"/g' "$mapfile" > "$modified"
if ! "$workdir/ivrvg" diff "$mapfile" "$modified" >"$workdir/diff-out.txt" 2>&1; then
	echo "  ok   diff of changed maps exits non-zero"
	echo "  --- diff output ---"
	cat "$workdir/diff-out.txt"
	echo "  --- end diff ---"
else
	echo "  FAIL diff should have detected changes"
	fail=1
fi

echo "==> checking Asterisk saw the calls"
if docker logs "$container" 2>&1 | grep -q "stub-ivr: greeting"; then
	echo "  ok   Asterisk ran the stub IVR"
else
	echo "  FAIL Asterisk never entered the stub-ivr context"
	fail=1
fi

if [ "$fail" -eq 0 ]; then
	echo "==> outbound integration test passed"
else
	echo "==> outbound integration test FAILED" >&2
fi
exit "$fail"
