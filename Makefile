BINARY ?= excalidraw-mcp
IMAGE ?= excalidraw.mcp

.PHONY: all web build test run serve docker clean

all: build

web:
	cd web && npm ci && npm run build

build: web
	go build -o $(BINARY) ./cmd/excalidraw-mcp

test: web
	go vet ./...
	go test ./...

run: build
	./$(BINARY) web

serve: build
	./$(BINARY) serve

docker:
	docker build -t $(IMAGE):local .

clean:
	rm -f $(BINARY)
	rm -rf internal/webui/dist web/node_modules
