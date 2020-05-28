SHELL:=/usr/bin/env bash
GO_FILES:=$(shell find . -name '*.go' | grep -v /vendor/)

default: help

# via https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
.PHONY: help
help: ## Print help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: all
all: clean build build-linux-amd64 build-darwin-amd64 ## Build for macOS and Linux on amd64

.PHONY: clean
clean: ## Remove built products in ./out
	rm -rf ./out

.PHONY: lint
lint: ## Lint all .go files
	@for file in ${GO_FILES} ;  do \
		echo "$$file" ; \
		golint $$file ; \
	done

.PHONY: build
build: lint ## Build (for the current platform & architecture) to ./out
	mkdir -p out
	go build -o ./out/ .

.PHONY: build-linux-amd64
build-linux-amd64:
	mkdir -p out/linux-amd64
	env GOOS=linux GOARCH=amd64 go build -o ./out/linux-amd64/ .

.PHONY: build-darwin-amd64
build-darwin-amd64:
	mkdir -p out/darwin-amd64
	env GOOS=darwin GOARCH=amd64 go build -o ./out/darwin-amd64/ .

.PHONY: install
	go build -o /usr/local/bin .
install: lint ## Build & install runner to /usr/local/bin
