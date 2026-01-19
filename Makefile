.PHONY: build install clean test lint e2e docs site clean-docs

BINARY := fab
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/tessro/fab/internal/version.Version=$(VERSION) \
           -X github.com/tessro/fab/internal/version.Commit=$(COMMIT) \
           -X github.com/tessro/fab/internal/version.Date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/fab

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/fab

clean:
	rm -f $(BINARY)

test:
	go test ./...

e2e:
	go test -v -count=1 ./internal/e2e

lint:
	golangci-lint run

docs:
	go run ./cmd/docsite --source docs --out site/public/docs

site: docs

clean-docs:
	rm -rf site/public/docs
