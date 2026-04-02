APP := xraycfg
DIST := dist
PKG := ./cmd/xraycfg
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-linux build-linux-amd64 build-linux-arm64 clean test

build: build-linux-amd64

build-linux: build-linux-amd64 build-linux-arm64

build-linux-amd64:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-amd64 $(PKG)

build-linux-arm64:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-arm64 $(PKG)

test:
	go test ./...

clean:
	rm -rf $(DIST)
