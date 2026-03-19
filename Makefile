BINARY_NAME=oxy
BUILD_DIR=.
GO=go
GOFLAGS=-v
PREFIX=/usr/local

.PHONY: build test lint clean install uninstall help

## build: Compile the binary
build:
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/oxy/

## test: Run all unit tests
test:
	$(GO) test $(GOFLAGS) ./...

## test-cover: Run tests with coverage report
test-cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

## lint: Run go vet
lint:
	$(GO) vet ./...

## install: Install binary to PREFIX/bin (default /usr/local/bin)
install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BUILD_DIR)/$(BINARY_NAME) $(PREFIX)/bin/$(BINARY_NAME)

## uninstall: Remove binary from PREFIX/bin
uninstall:
	rm -f $(PREFIX)/bin/$(BINARY_NAME)

## clean: Remove build artifacts
clean:
	rm -f $(BUILD_DIR)/$(BINARY_NAME)
	rm -f coverage.out coverage.html

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
