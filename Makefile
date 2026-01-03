.PHONY: build install clean test lint

BINARY := fab
PREFIX ?= /usr/local

build:
	go build -o $(BINARY) ./cmd/fab

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BINARY) $(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

clean:
	rm -f $(BINARY)

test:
	go test ./...

lint:
	golangci-lint run
