.PHONY: test lint build fuzz

test:
	go test -race ./...

lint:
	go vet ./... && staticcheck ./...

build:
	go build -o bin/cui-proxy ./cmd/cui-proxy

# 20 s fuzz smoke per target, as in CI.
fuzz:
	go test -run '^$$' -fuzz '^FuzzSanitizeRequest$$' -fuzztime 20s ./internal/adapter/codex
	go test -run '^$$' -fuzz '^FuzzSSERewrite$$' -fuzztime 20s ./internal/adapter/codex
	go test -run '^$$' -fuzz '^FuzzRoundTrip$$' -fuzztime 20s ./internal/transform
