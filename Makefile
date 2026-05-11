.PHONY: build go-build test web-build

build: go-build

go-build: web-build
	go build ./server/cmd/aggr

test: web-build
	go test ./...

web-build:
	pnpm --dir web build
