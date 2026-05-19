VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build release clean test

build:
	go build -ldflags "$(LDFLAGS)" -o dist/agentgit ./cmd/agentgit

release: clean
	mkdir -p dist
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/agentgit_$(VERSION)_darwin_amd64 ./cmd/agentgit
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/agentgit_$(VERSION)_darwin_arm64 ./cmd/agentgit
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/agentgit_$(VERSION)_linux_amd64 ./cmd/agentgit
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/agentgit_$(VERSION)_linux_arm64 ./cmd/agentgit

test:
	go test ./...

clean:
	rm -rf dist
