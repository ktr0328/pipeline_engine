.PHONY: test go-test sdk-test engine-test run dev build

GO ?= go
NPM ?= npm
APP_ADDR ?= :8085
SDK_TS_DIR := pkg/sdk/typescript
ENGINE_TS_DIR := pkg/engine/typescript

test: go-test sdk-test engine-test

## Run all Go unit tests
go-test:
	$(GO) test ./...

$(SDK_TS_DIR)/node_modules: $(SDK_TS_DIR)/package-lock.json
	cd $(SDK_TS_DIR) && $(NPM) install --no-fund --no-audit

$(ENGINE_TS_DIR)/node_modules: $(ENGINE_TS_DIR)/package-lock.json
	cd $(ENGINE_TS_DIR) && $(NPM) install --no-fund --no-audit

sdk-test: $(SDK_TS_DIR)/node_modules
	cd $(SDK_TS_DIR) && $(NPM) test

engine-test: $(ENGINE_TS_DIR)/node_modules
	cd $(ENGINE_TS_DIR) && $(NPM) test

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
