.PHONY: all build run run-asterisk replay sample test test-go test-ts typecheck \
        check fmt fmt-check vet schema-check integration-test outbound-test escalation-test eval diff poc

# What CI runs. Use this before pushing.
check: fmt-check vet build test-go typecheck test-ts schema-check eval

all: check

build:
	go build ./...

# Media over the WebSocket only — no telephony needed.
run:
	go run ./cmd/ivrvg serve

# Attached to a local Asterisk: media on 8090, control over the manager
# interface. Needs IVRVG_AMI_SECRET; see .env.example and docs/ASTERISK.md.
run-asterisk:
	go run ./cmd/ivrvg serve -audiosocket 127.0.0.1:8090 -ami 127.0.0.1:5038

# Replay a recorded call. Pass CALL=path/to/call.jsonl.
CALL ?= testdata/calls/example.jsonl
replay:
	go run ./cmd/ivrvg replay $(CALL)

sample:
	go run ./cmd/ivrvg sample

test: test-go test-ts

test-go:
	go test -race ./...

test-ts:
	cd sdk/ts && npm ci --no-audit --no-fund && node --test test/*.test.ts

typecheck:
	cd sdk/ts && npx tsc --noEmit

vet:
	go vet ./...

fmt:
	gofmt -w .

# Verify rather than rewrite, which is what CI needs.
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi

# A real Asterisk in a container, a real call, real DTMF. Needs Docker.
integration-test:
	./deploy/asterisk/docker/run-integration-test.sh

# Outbound test: map a stub IVR, verify the flow map, prove diff. Needs Docker.
outbound-test:
	./deploy/asterisk/docker/run-outbound-test.sh

# Escalation test: caller presses 3, agent escalates with a handoff packet,
# console receives the event, Asterisk redirects the channel. Needs Docker.
escalation-test:
	./deploy/asterisk/docker/run-escalation-test.sh

# Eval: run the scenario bank against recorded fixtures. No network needed.
eval:
	go run ./cmd/ivrvg eval -bank testdata/scenarios/eval-bank.json -fixtures testdata/fixtures

# Compare two flow maps; exit 1 if they differ. Pass BEFORE= and AFTER=.
BEFORE ?= before.json
AFTER  ?= after.json
diff:
	go run ./cmd/ivrvg diff $(BEFORE) $(AFTER)

schema-check:
	python3 -c "import json; json.load(open('schema/ivrvg.v1.schema.json'))"

# Phone-testable POC: Docker Asterisk + gateway with real providers.
# Set your API keys in .env and `source .env` first; see .env.example.
poc:
	./deploy/poc/run-poc.sh
