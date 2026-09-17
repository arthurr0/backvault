VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  = -s -w -X github.com/arthurr0/backvault/internal/version.Version=$(VERSION) -X github.com/arthurr0/backvault/internal/version.Commit=$(COMMIT) -X github.com/arthurr0/backvault/internal/version.BuildDate=$(DATE)

WEB_PLACEHOLDER = web/dist/index.html

.PHONY: all web web-placeholder build test race lint lint-scripts fmt dev-server dev-web docker clean

all: build

web:
	cd web && npm ci && npm run build

web-placeholder:
	@if [ ! -f "$(WEB_PLACEHOLDER)" ]; then \
		mkdir -p web/dist; \
		printf '%s\n' \
			'<!doctype html>' \
			'<html lang="en"><head><meta charset="utf-8"><title>Backvault</title></head>' \
			'<body><h1>Backvault</h1><p>Every backup, accounted for.</p>' \
			'<p>The admin panel has not been built into this binary. Run make web and rebuild.</p>' \
			'<p>The API is available under /api/v1.</p></body></html>' \
			> "$(WEB_PLACEHOLDER)"; \
	fi

build: web-placeholder
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/backvault ./cmd/backvault

test: web-placeholder
	go test ./...

race: web-placeholder
	go test -race ./...

lint: web-placeholder
	go vet ./...
	gofmt -l cmd internal web/embed.go
	cd web && npm run typecheck && npm run lint
	$(MAKE) lint-scripts

fmt:
	gofmt -w cmd internal web/embed.go

dev-server: web-placeholder
	go run ./cmd/backvault serve --data-dir ./data

dev-web:
	cd web && npm run dev

docker:
	docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(DATE) -t backvault:$(VERSION) .

lint-scripts:
	for f in scripts/*.sh scripts/lib/*.sh deploy/install.sh; do bash -n "$$f" || exit 1; done

clean:
	rm -rf bin web/dist backvault
