.PHONY: build test run demo docker fmt vet clean

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
  -X github.com/jsabella/gridwatch/internal/buildinfo.Version=$(VERSION) \
  -X github.com/jsabella/gridwatch/internal/buildinfo.Commit=$(COMMIT) \
  -X github.com/jsabella/gridwatch/internal/buildinfo.Date=$(DATE)

build: ## Compile the binary into ./gridwatch
	go build -trimpath -ldflags="$(LDFLAGS)" -o gridwatch ./cmd/gridwatch

test: ## Run unit tests with race detector
	go test -race ./...

run: build ## Run with the example config
	./gridwatch --config configs/gridwatch.example.yaml

demo: build ## Run with canned fixture data (no upstream polling)
	./gridwatch --config configs/gridwatch.example.yaml --demo

fmt: ## gofmt everything
	gofmt -s -w .

vet:
	go vet ./...

docker: ## Build a local Docker image
	docker build \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg COMMIT=$(COMMIT) \
	  --build-arg DATE=$(DATE) \
	  -t gridwatch:$(VERSION) .

clean:
	rm -f gridwatch
	rm -rf dist/
