PLUGIN_ID ?= codex-workspace-usage
VERSION ?= 0.2.0
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
DIST_DIR ?= $(CURDIR)/dist
GO_LDFLAGS ?= -s -w -X main.pluginVersion=$(VERSION)

EXT_linux = so
EXT_freebsd = so
EXT_darwin = dylib
EXT_windows = dll
PLUGIN_EXT = $(or $(EXT_$(GOOS)),so)
PLUGIN_FILE ?= $(DIST_DIR)/$(PLUGIN_ID).$(PLUGIN_EXT)
PLUGIN_HEADER = $(basename $(PLUGIN_FILE)).h
ARCHIVE_FILE = $(DIST_DIR)/$(PLUGIN_ID)_$(VERSION)_$(GOOS)_$(GOARCH).zip

.PHONY: build package test check clean

build:
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -trimpath -buildmode=c-shared -ldflags "$(GO_LDFLAGS)" -o $(PLUGIN_FILE) .
	rm -f $(PLUGIN_HEADER)

package: build
	go run ./.github/scripts/package-release.go -library $(PLUGIN_FILE) -archive $(ARCHIVE_FILE) -checksum $(ARCHIVE_FILE).sha256

test:
	go test ./...

check: test
	go vet ./...

clean:
	rm -rf $(DIST_DIR)
