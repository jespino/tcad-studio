# TCAD Studio — Makefile
#
# make            compila bin/tcad
# make help       lista todos los objetivos

GO      ?= go
BIN     := bin/tcad
STUDIO  := bin/tcad-studio
OUT     := out
PKG     := ./cmd/tcad
SOURCES := $(shell find cmd internal -type f \( -name '*.go' -o -name '*.html' -o -name '*.js' -o -name '*.mjs' -o -name '*.css' -o -name '*.md' \)) \
           embed.go $(wildcard .claude/skills/tcad/*.md) go.mod go.sum

# TCAD Studio (cmd/tcad-studio) is a Wails 3 app: it needs cgo and, on Linux,
# GTK3 / WebKitGTK 4.1 (libgtk-3-dev, libwebkit2gtk-4.1-dev). GO_TAGS= builds
# against Wails' default GTK4 / WebKitGTK 6.0 instead.
GO_TAGS ?= $(if $(filter Linux,$(shell uname -s)),gtk3)
TAGS    := -tags "$(GO_TAGS)"
# Carpeta que abre "make run-studio" / "make studio-web" (vacío: pantalla de inicio).
FOLDER  ?=

EXAMPLES := $(wildcard examples/*.tcad)
STLS     := $(patsubst examples/%.tcad,$(OUT)/%.stl,$(EXAMPLES))
PNGS     := $(patsubst examples/%.tcad,$(OUT)/%.png,$(EXAMPLES))

# Modelo(s) y dirección para "make watch", p. ej. make watch MODEL=diseños/pieza.tcad
# o, para elegir entre varios en la página, make watch MODEL="-R diseños examples"
MODEL ?= examples/caja_con_tapa.tcad
ADDR  ?= localhost:8080

# Versión de three.js incluida en el visor de "tcad watch".
THREE_VERSION ?= 0.160.0
THREE_URL     := https://unpkg.com/three@$(THREE_VERSION)
ASSETS        := internal/watch/assets
# Versión de xterm.js del terminal del agente en TCAD Studio.
XTERM_VERSION    ?= 6.0.0
XTERM_FIT_VERSION ?= 0.11.0
STUDIO_ASSETS    := internal/studio/assets

.DEFAULT_GOAL := build

.PHONY: help build install studio install-studio run-studio studio-web test race vet fmt fmt-check check \
        examples renders watch update-three update-xterm clean

help: ## Muestra esta ayuda
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: $(BIN) ## Compila bin/tcad

$(BIN): $(SOURCES)
	@mkdir -p $(dir $@)
	$(GO) build -o $@ $(PKG)

install: ## Instala tcad en $GOPATH/bin (o $GOBIN)
	$(GO) install $(PKG)

studio: $(STUDIO) ## Compila la app de escritorio bin/tcad-studio (Wails 3)

$(STUDIO): $(SOURCES)
	@mkdir -p $(dir $@)
	$(GO) build $(TAGS) -o $@ ./cmd/tcad-studio

install-studio: ## Instala tcad-studio en $GOPATH/bin (o $GOBIN)
	$(GO) install $(TAGS) ./cmd/tcad-studio

run-studio: $(STUDIO) ## Abre TCAD Studio: make run-studio [FOLDER=diseños]
	$(STUDIO) $(FOLDER)

studio-web: $(BIN) ## TCAD Studio en el navegador: make studio-web [FOLDER=diseños]
	$(BIN) studio -open $(FOLDER)

test: ## Ejecuta los tests
	$(GO) test $(TAGS) ./...

race: ## Ejecuta los tests con el detector de carreras
	$(GO) test $(TAGS) -race ./...

# -composites=false: los literales Vec2/Vec3 sin nombres de campo son
# intencionados (son alias de tipos de geom).
vet: ## Análisis estático con go vet
	$(GO) vet $(TAGS) -composites=false ./...

fmt: ## Formatea el código con gofmt
	gofmt -w cmd internal embed.go

fmt-check: ## Falla si hay ficheros sin formatear
	@out=$$(gofmt -l cmd internal embed.go); if [ -n "$$out" ]; then echo "sin formatear:"; echo "$$out"; exit 1; fi

check: fmt-check vet test examples ## Todo lo anterior + compilar los ejemplos (para CI)

examples: $(STLS) ## Genera los STL de examples/ en out/

renders: $(PNGS) ## Genera las previsualizaciones PNG de examples/ en out/

$(OUT)/%.stl: examples/%.tcad $(BIN)
	@mkdir -p $(OUT)
	$(BIN) build $< -o $@

$(OUT)/%.png: examples/%.tcad $(BIN)
	@mkdir -p $(OUT)
	$(BIN) render $< -o $@

watch: $(BIN) ## Visor en vivo: make watch MODEL="diseños/pieza.tcad | -R diseños" [ADDR=host:puerto]
	$(BIN) watch $(MODEL) -addr $(ADDR) -open

update-three: ## Actualiza three.js del visor: make update-three THREE_VERSION=x.y.z
	curl -sfL -o $(ASSETS)/three.module.min.js $(THREE_URL)/build/three.module.min.js
	curl -sfL -o $(ASSETS)/OrbitControls.js $(THREE_URL)/examples/jsm/controls/OrbitControls.js
	curl -sfL -o $(ASSETS)/STLLoader.js $(THREE_URL)/examples/jsm/loaders/STLLoader.js
	curl -sfL -o $(ASSETS)/LICENSE-three.txt $(THREE_URL)/LICENSE
	@echo "three.js $(THREE_VERSION) descargado; revisa el visor con 'make watch'"

update-xterm: ## Actualiza xterm.js del terminal del estudio: make update-xterm XTERM_VERSION=x.y.z
	curl -sfL -o $(STUDIO_ASSETS)/xterm.mjs https://unpkg.com/@xterm/xterm@$(XTERM_VERSION)/lib/xterm.mjs
	curl -sfL -o $(STUDIO_ASSETS)/xterm.css https://unpkg.com/@xterm/xterm@$(XTERM_VERSION)/css/xterm.css
	curl -sfL -o $(STUDIO_ASSETS)/LICENSE-xterm.txt https://unpkg.com/@xterm/xterm@$(XTERM_VERSION)/LICENSE
	curl -sfL -o $(STUDIO_ASSETS)/addon-fit.mjs https://unpkg.com/@xterm/addon-fit@$(XTERM_FIT_VERSION)/lib/addon-fit.mjs
	@echo "xterm.js $(XTERM_VERSION) descargado; revisa el terminal con 'make run-studio'"

clean: ## Borra bin/ y out/
	rm -rf bin $(OUT)
