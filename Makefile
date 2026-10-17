.PHONY: build test test-go test-ts typecheck gofmt go-vet

build:
	go build ./...

test-go:
	go test -race ./...

test-ts:
	cd sdk/ts && npm install --no-audit --no-fund && node --test test/*.test.ts

typecheck:
	cd sdk/ts && npx tsc --noEmit

test: test-go test-ts

go-vet:
	go vet ./...

gofmt:
	gofmt -w .
