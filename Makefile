TAILWIND_VERSION := v4.3.3
UNAME_S := $(shell uname -s | tr A-Z a-z | sed 's/darwin/macos/')
UNAME_M := $(shell uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/')
TAILWIND := bin/tailwindcss
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: gen build build-linux build-darwin test lint run site

$(TAILWIND):
	mkdir -p bin
	curl -fsSL -o $@ https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(UNAME_S)-$(UNAME_M)
	chmod +x $@

gen: $(TAILWIND)
	go tool templ generate
	$(TAILWIND) -i assets/css/globals.css -o assets/css/output.css --minify

build: gen
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yasssm .

# Linux servers (amd64) and Apple silicon Macs.
build-linux: gen
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yasssm-linux-amd64 .

build-darwin: gen
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yasssm-darwin-arm64 .

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l . | grep -v _templ.go)"

run: build
	./bin/yasssm

# Project website (GitHub Pages) → site/dist, built from the app's own components and theme.
site: gen
	go tool templ generate
	rm -rf site/dist && mkdir -p site/dist/assets/css
	$(TAILWIND) -i site/site.css -o site/dist/assets/css/site.css --minify
	go run ./site/build
