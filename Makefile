.PHONY: build go-build test web-dev web-build fmt tidy

build: go-build

go-build: web-build
	go build ./server/cmd/aggr

test: web-build
	go test ./...

web-dev:
	pnpm --dir web dev

web-build:
	pnpm --dir web build

fmt:
	gofmt -w server tests

tidy:
	go mod tidy
