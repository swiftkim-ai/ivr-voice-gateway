#!/usr/bin/env bash
# POC: real phone call through the IVR Voice Gateway with AI providers.
#
# Prerequisites:
#   - Docker
#   - Go 1.23+
#   - A SIP softphone (Linphone, Zoiper, Orin)
#   - At least one LLM API key in the environment (see .env.example)
#
# Usage:
#   # Fill in your API keys:
#   cp .env.example .env
#   $EDITOR .env
#   source .env
#   make poc
#
# The script starts Asterisk in Docker and the gateway with the best available
# providers. Register your softphone to 127.0.0.1 and dial 7000.

set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
docker_dir="$repo/deploy/asterisk/docker"
image=ivrvg-asterisk:poc
container=ivrvg-asterisk-poc
workdir="$(mktemp -d)"
logdir="$workdir/calls"

media_port=8090
ami_port=5038
sip_port=5060
rtp_start=10000
rtp_end=10020
secret=testsecret

cleanup() {
    echo ""
    echo "==> shutting down"
    [ -n "${gateway_pid:-}" ] && kill "$gateway_pid" 2>/dev/null || true
    docker rm -f "$container" >/dev/null 2>&1 || true
    echo "    call logs: $logdir"
}
trap cleanup EXIT INT TERM

# --- detect the best available providers ---

brain=demo
brain_label="demo (scripted — set an LLM key for real AI)"
for var_brain in \
    "IVRVG_ANTHROPIC_KEY:claude:claude (Anthropic)" \
    "IVRVG_OPENAI_KEY:openai:openai (OpenAI)" \
    "IVRVG_GROQ_KEY:groq:groq (Groq)"; do
    var="${var_brain%%:*}"
    rest="${var_brain#*:}"
    name="${rest%%:*}"
    label="${rest#*:}"
    if [ -n "${!var:-}" ]; then
        brain="$name"
        brain_label="$label"
        break
    fi
done

tts_label="silence (set IVRVG_ELEVENLABS_KEY + IVRVG_ELEVENLABS_VOICE)"
has_tts=false
if [ -n "${IVRVG_ELEVENLABS_KEY:-}" ] && [ -n "${IVRVG_ELEVENLABS_VOICE:-}" ]; then
    tts_label="elevenlabs"
    has_tts=true
fi

stt_label="off (set IVRVG_DEEPGRAM_KEY to enable)"
if [ -n "${IVRVG_DEEPGRAM_KEY:-}" ]; then
    stt_label="deepgram"
fi

echo "==> IVR Voice Gateway POC"
echo "    brain: $brain_label"
echo "    tts:   $tts_label"
echo "    stt:   $stt_label"
echo ""

# --- check ports ---

for port in "$media_port" "$ami_port" "$sip_port"; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1 || \
       lsof -nP -iUDP:"$port" >/dev/null 2>&1; then
        echo "port $port is already in use; stop whatever holds it and retry:" >&2
        lsof -nP -i:"$port" >&2
        exit 1
    fi
done

# --- build ---

echo "==> building the Docker switch"
docker build -q -t "$image" "$docker_dir" >/dev/null

echo "==> building the gateway"
(cd "$repo" && go build -o "$workdir/ivrvg" ./cmd/ivrvg)

# --- start Asterisk with SIP + AMI ---

echo "==> starting Asterisk (SIP on :$sip_port, AMI on :$ami_port)"
docker rm -f "$container" >/dev/null 2>&1 || true

rtp_ports=()
for ((p=rtp_start; p<=rtp_end; p++)); do
    rtp_ports+=(-p "$p:$p/udp")
done

docker run -d --name "$container" \
    --add-host=host.docker.internal:host-gateway \
    -p "127.0.0.1:$ami_port:5038" \
    -p "$sip_port:5060/udp" \
    -p "$sip_port:5060/tcp" \
    "${rtp_ports[@]}" \
    -v "$docker_dir/extensions.conf:/etc/asterisk/extensions.conf:ro" \
    -v "$docker_dir/manager.conf:/etc/asterisk/manager.conf:ro" \
    -v "$docker_dir/pjsip.conf:/etc/asterisk/pjsip.conf:ro" \
    -v "$docker_dir/rtp.conf:/etc/asterisk/rtp.conf:ro" \
    "$image" >/dev/null

echo "==> waiting for the switch"
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

if docker exec "$container" asterisk -rx "pjsip show endpoints" 2>/dev/null | grep -q softphone; then
    echo "    PJSIP endpoint 'softphone' ready"
else
    echo "    warning: PJSIP endpoint not detected; check Asterisk logs:"
    echo "    docker logs $container"
fi

docker exec "$container" asterisk -rx "core show version" | head -1

# --- start the gateway ---

echo "==> starting the gateway"
mkdir -p "$logdir"

gateway_args=(
    serve
    -addr 127.0.0.1:8099
    -audiosocket ":$media_port"
    -ami "127.0.0.1:$ami_port"
    -ami-context default
    -brain "$brain"
    -logdir "$logdir"
)
if [ "$has_tts" = true ]; then
    gateway_args+=(-tts elevenlabs)
fi

IVRVG_AMI_SECRET="$secret" \
    "$workdir/ivrvg" "${gateway_args[@]}" >"$workdir/gateway.log" 2>&1 &
gateway_pid=$!

i=0
until grep -q "listening on" "$workdir/gateway.log" 2>/dev/null; do
    i=$((i + 1))
    if [ "$i" -gt 30 ]; then
        echo "the gateway did not start:" >&2
        cat "$workdir/gateway.log" >&2
        exit 1
    fi
    sleep 1
done

echo ""
echo "============================================================"
echo "  POC READY"
echo "============================================================"
echo ""
echo "  1. Register your softphone:"
echo "       Server:    127.0.0.1"
echo "       Username:  softphone"
echo "       Password:  ivrvg1234"
echo "       Transport: UDP"
echo ""
echo "  2. Dial 7000"
echo ""
if [ "$brain" = "demo" ]; then
    echo "  The demo brain responds to DTMF keypresses:"
    echo "    1 = transfer to sales"
    echo "    2 = enter account number"
    echo "    3 = billing escalation"
    echo "    0 = operator"
    echo ""
fi
echo "  Console:   http://127.0.0.1:8099/v1/console/ui"
echo "  Call logs:  $logdir"
echo ""
echo "  Press Ctrl+C to stop."
echo "============================================================"
echo ""

# Show live gateway output so the user sees what's happening.
tail -f "$workdir/gateway.log"
