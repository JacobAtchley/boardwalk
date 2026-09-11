BINARY := boardwalk
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet fmt install clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

install: build
	install -m 0755 $(BINARY) $(HOME)/.local/bin/$(BINARY)

clean:
	rm -f $(BINARY)
