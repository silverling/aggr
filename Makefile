.PHONY: build go-build test dev web-dev web-build fmt tidy release-linux-amd64 release-linux-arm64

VERSION ?= $(shell ./scripts/build-version.sh)
GO_LDFLAGS := -X github.com/silverling/aggr/server.buildVersion=$(VERSION)

build: go-build

go-build: web-build
	go build -ldflags "$(GO_LDFLAGS)" ./server/cmd/aggr

test: web-build
	go test ./...

dev:
	@trap 'kill 0' INT TERM EXIT; \
	pnpm --dir web dev & \
	AGGR_ENV=dev go run -ldflags "$(GO_LDFLAGS)" ./server/cmd/aggr

web-dev:
	pnpm --dir web dev

web-build:
	pnpm --dir web build

fmt:
	gofmt -w server tests

tidy:
	go mod tidy

release-linux-amd64:
	VERSION="$(VERSION)" ./scripts/package-release.sh linux amd64

release-linux-arm64:
	VERSION="$(VERSION)" ./scripts/package-release.sh linux arm64
