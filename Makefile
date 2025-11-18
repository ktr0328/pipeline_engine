.PHONY: test run dev build

GO ?= go
APP_ADDR ?= :8085

## Run all Go unit tests
test:
	$(GO) test ./...

## Run the pipeline engine HTTP server
run:
	PIPELINE_ENGINE_ADDR=$(APP_ADDR) $(GO) run ./cmd/pipeline-engine

## Run tests and then launch the server (basic dev loop)
dev:
	@echo "==> Running tests"
	$(GO) test ./...
	@echo "==> Starting pipeline engine on $(APP_ADDR)"
	PIPELINE_ENGINE_ADDR=$(APP_ADDR) $(GO) run ./cmd/pipeline-engine

## Build the pipeline engine binary
build:
	$(GO) build ./cmd/pipeline-engine
