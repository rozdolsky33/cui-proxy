.PHONY: test lint build

test:
	go test -race ./...

lint:
	go vet ./... && staticcheck ./...

build:
	go build -o bin/cui-proxy ./cmd/cui-proxy
