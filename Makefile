BINARY   := marga
CMD      := ./cmd/marga
GO       := go
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"
BUILD_DIR := bin
# goolm selects mautrix's pure-Go Olm backend so end-to-end encryption builds
# without a CGo dependency on system libolm. CGO_ENABLED=0 keeps binaries static.
TAGS     := goolm
export CGO_ENABLED ?= 0

.PHONY: build install clean test lint fmt vet check cross-build

build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -tags '$(TAGS)' $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)

install: build
	$(GO) install -tags '$(TAGS)' $(LDFLAGS) $(CMD)

clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean

test:
	$(GO) test -tags '$(TAGS)' ./...

lint: vet fmt

vet:
	$(GO) vet -tags '$(TAGS)' ./...

fmt:
	$(GO)fmt -w .

check: vet test

cross-build:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64 $(GO) build -tags '$(TAGS)' $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-amd64   $(CMD)
	GOOS=linux   GOARCH=arm64 $(GO) build -tags '$(TAGS)' $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-arm64   $(CMD)
	GOOS=darwin  GOARCH=amd64 $(GO) build -tags '$(TAGS)' $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-darwin-amd64  $(CMD)
	GOOS=darwin  GOARCH=arm64 $(GO) build -tags '$(TAGS)' $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-darwin-arm64  $(CMD)
	GOOS=windows GOARCH=amd64 $(GO) build -tags '$(TAGS)' $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe $(CMD)
	GOOS=windows GOARCH=arm64 $(GO) build -tags '$(TAGS)' $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-windows-arm64.exe $(CMD)