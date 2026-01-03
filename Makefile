.PHONY: build install clean test lint

BINARY := fab

build:
	go build -o $(BINARY) ./cmd/fab

install:
	go install ./cmd/fab

clean:
	rm -f $(BINARY)

test:
	go test ./...

lint:
	golangci-lint run
