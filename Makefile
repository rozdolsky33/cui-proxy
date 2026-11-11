.PHONY: test lint build fuzz vuln

test:
	go test -race -run . -bench '^BenchmarkAuditDurability$$' -benchtime=4x ./...

lint:
	go vet ./... && staticcheck ./...

build:
	go build -o bin/cui-proxy ./cmd/cui-proxy

# 20 s fuzz smoke per target, as in CI.
fuzz:
	go test -run '^$$' -fuzz '^FuzzSanitizeRequest$$' -fuzztime 20s ./internal/adapter/codex
	go test -run '^$$' -fuzz '^FuzzSSERewrite$$' -fuzztime 20s ./internal/adapter/codex
	go test -run '^$$' -fuzz '^FuzzArgumentJSON$$' -fuzztime 20s ./internal/adapter/codex
	go test -run '^$$' -fuzz '^FuzzRoundTrip$$' -fuzztime 20s ./internal/transform

# Scan reachable code against the current Go vulnerability database.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
