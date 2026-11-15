BINARY := blackflame
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/galq/blackflame/internal/version.Version=$(VERSION) \
	-X github.com/galq/blackflame/internal/version.Commit=$(COMMIT) \
	-X github.com/galq/blackflame/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: build test race vet fmt run validate import export backup docker clean cross

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/blackflame

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

run:
	go run ./cmd/blackflame serve --mode ui --data ./data --listen 127.0.0.1:8080

validate:
	go run ./cmd/blackflame validate --config ./blackflame.yaml

import:
	go run ./cmd/blackflame import flame --db ./db.sqlite --data ./data

export:
	go run ./cmd/blackflame export --data ./data --output ./blackflame.yaml

backup:
	go run ./cmd/blackflame backup --data ./data --output ./blackflame-backup.db

docker:
	docker build -t blackflame:dev .

cross:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 ./cmd/blackflame
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/blackflame
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-armv7 ./cmd/blackflame

clean:
	rm -rf bin
