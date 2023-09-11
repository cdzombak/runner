SHELL:=/usr/bin/env bash
VERSION:=$(shell [ -z "$$(git tag --points-at HEAD)" ] && echo "$$(git describe --always --long --dirty | sed 's/^v//')" || echo "$$(git tag --points-at HEAD | sed 's/^v//')")
GO_FILES:=$(shell find . -name '*.go' | grep -v /vendor/)
BIN_NAME:=runner

default: help

# via https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
.PHONY: help
help: ## Print help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: all
all: clean build build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 ## Build for macOS and Linux

.PHONY: clean
clean: ## Remove build products (./out)
	rm -rf ./out

.PHONY: lint
lint: ## Lint all .go files
	@for file in ${GO_FILES} ;  do \
		echo "$$file" ; \
		golint $$file ; \
	done

.PHONY: build
build: ## Build for the current platform & architecture to ./out
	mkdir -p out
	go build -ldflags="-X main.version=${VERSION}" -o ./out/${BIN_NAME} .

.PHONY: build-linux-amd64
build-linux-amd64: ## Build for Linux/amd64 to ./out
	env GOOS=linux GOARCH=amd64 go build -ldflags="-X main.version=${VERSION}" -o ./out/${BIN_NAME}-linux-amd64 .

.PHONY: build-linux-arm64
build-linux-arm64: ## Build for Linux/arm64 to ./out
	env GOOS=linux GOARCH=arm64 go build -ldflags="-X main.version=${VERSION}" -o ./out/${BIN_NAME}-linux-arm64 .

.PHONY: build-darwin-amd64
build-darwin-amd64: ## Build for macOS/amd64 to ./out
	env GOOS=darwin GOARCH=amd64 go build -ldflags="-X main.version=${VERSION}" -o ./out/${BIN_NAME}-darwin-amd64 .

.PHONY: build-darwin-arm64
build-darwin-arm64: ## Build for macOS/arm64 to ./out
	env GOOS=darwin GOARCH=arm64 go build -ldflags="-X main.version=${VERSION}" -o ./out/${BIN_NAME}-darwin-arm64 .
