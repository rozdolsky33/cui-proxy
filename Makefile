.PHONY: test lint build fuzz vuln client-probe

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

# Run the installed Codex client against a local stand-in, directly and through
# bin/cui-proxy, and compare its requests with the recorded baseline. Contacts
# no model service. See docs/upstream-changes.md.
client-probe: build
	go run ./scripts/clientprobe -baseline docs/verification-results/client-shapes/baseline.json $(CLIENTPROBE_FLAGS)
