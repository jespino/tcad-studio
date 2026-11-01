# TCAD Studio — Makefile
#
# make            builds bin/tcad
# make help       lists every target

GO      ?= go
BIN     := bin/tcad
STUDIO  := bin/tcad-studio
OUT     := out
PKG     := ./cmd/tcad
SOURCES := $(shell find cmd internal -type f \( -name '*.go' -o -name '*.html' -o -name '*.js' -o -name '*.mjs' -o -name '*.css' -o -name '*.md' \)) \
           embed.go $(wildcard .agents/skills/tcad/*.md) $(shell find frontend/dist -type f 2>/dev/null) go.mod go.sum

# TCAD Studio (cmd/tcad-studio) is a Wails 3 app: it needs cgo and, on Linux,
# GTK3 / WebKitGTK 4.1 (libgtk-3-dev, libwebkit2gtk-4.1-dev). GO_TAGS= builds
# against Wails' default GTK4 / WebKitGTK 6.0 instead.
GO_TAGS ?= $(if $(filter Linux,$(shell uname -s)),gtk3)
TAGS    := -tags "$(GO_TAGS)"
# Folder opened by "make run" / "make studio-web" (empty: the start screen).
FOLDER  ?=

EXAMPLES := $(wildcard examples/*.tcad)
STLS     := $(patsubst examples/%.tcad,$(OUT)/%.stl,$(EXAMPLES))
PNGS     := $(patsubst examples/%.tcad,$(OUT)/%.png,$(EXAMPLES))

# Model(s) and address for "make watch", e.g. make watch MODEL=designs/part.tcad
# or, to pick among several in the page, make watch MODEL="-R designs examples"
MODEL ?= examples/box_with_lid.tcad
ADDR  ?= localhost:8080

# three.js version bundled in the "tcad watch" viewer.
THREE_VERSION ?= 0.160.0
THREE_URL     := https://unpkg.com/three@$(THREE_VERSION)
ASSETS        := internal/watch/assets
# Where "make dev" runs the studio's backend for Vite (frontend/vite.config.ts).
DEV_ADDR ?= 127.0.0.1:34118

.DEFAULT_GOAL := build

.PHONY: help build install studio install-studio run run-studio studio-web test race vet fmt fmt-check check \
        examples renders watch update-three frontend dev clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: $(BIN) ## Build bin/tcad

$(BIN): $(SOURCES)
	@mkdir -p $(dir $@)
	$(GO) build -o $@ $(PKG)

install: ## Install tcad into $GOPATH/bin (or $GOBIN)
	$(GO) install $(PKG)

studio: $(STUDIO) ## Build the desktop app bin/tcad-studio (Wails 3)

$(STUDIO): $(SOURCES)
	@mkdir -p $(dir $@)
	$(GO) build $(TAGS) -o $@ ./cmd/tcad-studio

install-studio: ## Install tcad-studio into $GOPATH/bin (or $GOBIN)
	$(GO) install $(TAGS) ./cmd/tcad-studio

run: $(STUDIO) ## Build TCAD Studio and start it from the repo: make run [FOLDER=designs]
	$(STUDIO) $(FOLDER)

run-studio: run ## Alias of "make run"

studio-web: $(BIN) ## TCAD Studio in the browser: make studio-web [FOLDER=designs]
	$(BIN) studio -open $(FOLDER)

frontend: ## Rebuild the studio UI into frontend/dist (committed, so go build needs no Node)
	cd frontend && npm ci && npm run build

dev: $(BIN) ## The studio UI with hot reload: tcad studio + Vite on http://127.0.0.1:5173 [FOLDER=designs]
	@[ -d frontend/node_modules ] || (cd frontend && npm ci)
	@trap 'kill 0' EXIT; export AGENTSTUDIO_TOKEN=$$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n'); \
	  $(BIN) studio -addr $(DEV_ADDR) $(FOLDER) & (cd frontend && npm run dev)

test: ## Run the tests
	$(GO) test $(TAGS) ./...

race: ## Run the tests with the race detector
	$(GO) test $(TAGS) -race ./...

# -composites=false: the Vec2/Vec3 literals without field names are
# intentional (they are aliases of geom types).
vet: ## Static analysis with go vet
	$(GO) vet $(TAGS) -composites=false ./...

fmt: ## Format the code with gofmt
	gofmt -w cmd internal embed.go

fmt-check: ## Fail if any file is not formatted
	@out=$$(gofmt -l cmd internal embed.go); if [ -n "$$out" ]; then echo "not formatted:"; echo "$$out"; exit 1; fi

check: fmt-check vet test examples ## All of the above + build the examples (for CI)

examples: $(STLS) ## Build the STLs of examples/ into out/

renders: $(PNGS) ## Render the PNG previews of examples/ into out/

$(OUT)/%.stl: examples/%.tcad $(BIN)
	@mkdir -p $(OUT)
	$(BIN) build $< -o $@

$(OUT)/%.png: examples/%.tcad $(BIN)
	@mkdir -p $(OUT)
	$(BIN) render $< -o $@

watch: $(BIN) ## Live viewer: make watch MODEL="designs/part.tcad | -R designs" [ADDR=host:port]
	$(BIN) watch $(MODEL) -addr $(ADDR) -open

update-three: ## Update the viewer's three.js: make update-three THREE_VERSION=x.y.z
	curl -sfL -o $(ASSETS)/three.module.min.js $(THREE_URL)/build/three.module.min.js
	curl -sfL -o $(ASSETS)/OrbitControls.js $(THREE_URL)/examples/jsm/controls/OrbitControls.js
	curl -sfL -o $(ASSETS)/STLLoader.js $(THREE_URL)/examples/jsm/loaders/STLLoader.js
	curl -sfL -o $(ASSETS)/LICENSE-three.txt $(THREE_URL)/LICENSE
	@echo "three.js $(THREE_VERSION) downloaded; check the viewer with 'make watch'"

clean: ## Remove bin/ and out/
	rm -rf bin $(OUT)
