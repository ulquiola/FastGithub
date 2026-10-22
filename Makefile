VERSION    ?= dev
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w -X fastgithub-go/internal/app.version=$(VERSION) -X fastgithub-go/internal/app.commit=$(COMMIT) -X fastgithub-go/internal/app.buildTime=$(BUILD_TIME)

.PHONY: all test vet fmt build ci release fetch-windivert bench macapp macapp-install clean

all: test build

test:
	go test ./...

bench:
	go test ./internal/config ./internal/resolver ./internal/mitm -run '^$$' -bench . -benchtime=1s

vet:
	go vet ./...

fmt:
	@test -z "$$(gofmt -l .)" || (gofmt -l .; exit 1)

build:
	go build -ldflags "$(LDFLAGS)" -o fastgithub ./cmd/fastgithub

ci:
	./scripts/ci.sh

release:
	./scripts/release.sh

fetch-windivert:
	./scripts/fetch-windivert.sh

macapp:
	./scripts/build-macos-app.sh

macapp-install:
	INSTALL=1 ./scripts/build-macos-app.sh

clean:
	rm -rf dist fastgithub fastgithub.exe
