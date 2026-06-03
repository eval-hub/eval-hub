.PHONY: help autoupdate-precommit pre-commit clean build build-coverage build-service build-init build-sidecar build-mcp build-all-platforms cross-compile-mcp build-all-platforms-mcp start-service stop-service start-sidecar stop-sidecar lint test test-fvt-server test-all test-coverage test-fvt-coverage test-fvt-server-coverage test-all-coverage install-deps update-deps get-deps fmt vet update-deps generate-public-docs verify-api-docs generate-ignore-file documentation check-unused-components fvt-report docker-image-local docker-mcp-version test-mcp-build-all test-mcp-binary-info test-mcp-binary-naming test-mcp-version test-mcp-no-runtime-deps test-mcp-container-build test-mcp-container-http test-mcp-checksums test-mcp-formula-syntax test-mcp-native-smoke test-mcp-brew-install test-mcp-brew-test test-mcp-brew-uninstall test-mcp-cross-platform test-mcp-e2e test-mcp test-mcp-vscode test-help

GOPATH := $(shell go env GOPATH)
GOBIN := $(shell go env GOPATH)/bin

# Variables
BINARY_NAME = eval-hub
CMD_PATH = ./cmd/eval_hub
INIT_BINARY_NAME = eval-runtime-init
INIT_CMD_PATH = ./cmd/eval_runtime_init
SIDECAR_BINARY_NAME = eval-runtime-sidecar
SIDECAR_CMD_PATH = ./cmd/eval_runtime_sidecar
MCP_BINARY_NAME = evalhub-mcp
MCP_CMD_PATH = ./cmd/evalhub_mcp
BIN_DIR = bin
PORT ?= 8080

# Default target
.DEFAULT_GOAL := help

# Auto-detect platform for cross-compilation and wheel building
# Uses Go's native platform detection - override by setting CROSS_GOOS/CROSS_GOARCH env vars if needed.
CROSS_GOOS ?= $(shell go env GOOS)
CROSS_GOARCH ?= $(shell go env GOARCH)

DATE ?= $(shell date +%FT%T%z)
GIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

help: ## Display this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

PRE_COMMIT ?= .git/hooks/pre-commit

${PRE_COMMIT}: .pre-commit-config.yaml
	pre-commit install

autoupdate-precommit:
	pre-commit autoupdate

pre-commit: autoupdate-precommit ${PRE_COMMIT}

CLEAN_OPTS ?= -r -cache -testcache # -x

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -f $(BINARY_NAME)
	@go clean ${CLEAN_OPTS}
	@rm -f ${GOBIN}/go-cover-treemap && true
	@echo "Clean complete"

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

BUILD_PACKAGE ?= main
FULL_BUILD_NUMBER ?= $(shell cat VERSION)
LDFLAGS_X = -X "${BUILD_PACKAGE}.Build=${FULL_BUILD_NUMBER}" -X "${BUILD_PACKAGE}.BuildDate=$(DATE)" -X "${BUILD_PACKAGE}.GitHash=${GIT_HASH}"
LDFLAGS = -buildmode=exe ${LDFLAGS_X}

build-service: $(BIN_DIR) ## Build the service binary
	@echo "Building $(BINARY_NAME) with ${LDFLAGS}"
	@go build -race -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

build-init: $(BIN_DIR) ## Build the eval-runtime-init binary only
	@echo "Building $(INIT_BINARY_NAME) with ${LDFLAGS}"
	@go build -race -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(INIT_BINARY_NAME) $(INIT_CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(INIT_BINARY_NAME)"

build: build-service build-init build-sidecar build-mcp ## Build the binaries

build-coverage: $(BIN_DIR) ## Build the binaries with coverage
	@echo "Building $(BINARY_NAME)-cov with -cover -covermode=atomic -ldflags ${LDFLAGS} "
	@go build -race -cover -covermode=atomic -coverpkg=./... -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(BINARY_NAME)-cov $(CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)-cov"
	@echo "Building $(INIT_BINARY_NAME)-cov with -cover -covermode=atomic -ldflags ${LDFLAGS} "
	@go build -race -cover -covermode=atomic -coverpkg=./... -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(INIT_BINARY_NAME)-cov $(INIT_CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(INIT_BINARY_NAME)-cov"
	@echo "Building $(SIDECAR_BINARY_NAME)-cov with -cover -covermode=atomic -ldflags ${LDFLAGS} "
	@go build -race -cover -covermode=atomic -coverpkg=./... -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(SIDECAR_BINARY_NAME)-cov $(SIDECAR_CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(SIDECAR_BINARY_NAME)-cov"

SERVER_PID_FILE ?= $(BIN_DIR)/pid

${SERVER_PID_FILE}:
	rm -f "${SERVER_PID_FILE}" && true

SERVICE_LOG ?= $(BIN_DIR)/service.log

start-service: test-setup ${SERVER_PID_FILE} build-service ## Run the application in background
	@echo "Running $(BINARY_NAME) on port $(PORT)..."
	@if [ -f $(VENV_DIR)/bin/activate ]; then . $(VENV_DIR)/bin/activate; else . $(VENV_DIR)/Scripts/activate; fi && ./scripts/start_server.sh "${SERVER_PID_FILE}" "${BIN_DIR}/$(BINARY_NAME)" "${SERVICE_LOG}" ${PORT} ""

start-service-coverage: test-setup ${SERVER_PID_FILE} build-coverage ## Run the application in background
	@echo "Running $(BINARY_NAME)-cov on port $(PORT)..."
	@if [ -f $(VENV_DIR)/bin/activate ]; then . $(VENV_DIR)/bin/activate; else . $(VENV_DIR)/Scripts/activate; fi && ./scripts/start_server.sh "${SERVER_PID_FILE}" "${BIN_DIR}/$(BINARY_NAME)-cov" "${SERVICE_LOG}" ${PORT} "${BIN_DIR}"

stop-service:
	-./scripts/stop_server.sh "${SERVER_PID_FILE}"
	! grep -i -F panic "${SERVICE_LOG}"

# Sidecar (eval-runtime-sidecar) starter/stopper
SIDECAR_PID_FILE ?= $(BIN_DIR)/sidecar.pid
SIDECAR_LOG ?= $(BIN_DIR)/sidecar.log
SIDECAR_PORT ?= 8081
# Config dir containing sidecar_runtime_local.json (or minimal JSON is generated from SIDECAR_PORT)
SIDECAR_CONFIG_DIR ?= config

build-sidecar: $(BIN_DIR) ## Build only the sidecar binary
	@echo "Building $(SIDECAR_BINARY_NAME) with ${LDFLAGS}"
	@go build -race -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(SIDECAR_BINARY_NAME) $(SIDECAR_CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(SIDECAR_BINARY_NAME)"

build-mcp: $(BIN_DIR) ## Build the evalhub-mcp MCP server binary
	@echo "Building $(MCP_BINARY_NAME) with ${LDFLAGS}"
	@go build -race -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(MCP_BINARY_NAME) $(MCP_CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(MCP_BINARY_NAME)"

start-sidecar: build-sidecar ## Run the sidecar in background (port $(SIDECAR_PORT), config from $(SIDECAR_CONFIG_DIR))
	@rm -f "${SIDECAR_PID_FILE}" && true
	@echo "Running $(SIDECAR_BINARY_NAME) on port $(SIDECAR_PORT) (config: $(SIDECAR_CONFIG_DIR))..."
	@SIDECAR_PORT="$(SIDECAR_PORT)" ./scripts/start_sidecar.sh "${SIDECAR_PID_FILE}" "${BIN_DIR}/$(SIDECAR_BINARY_NAME)" "${SIDECAR_LOG}" "$(SIDECAR_PORT)" "$(SIDECAR_CONFIG_DIR)"

stop-sidecar: ## Stop the sidecar
	-./scripts/stop_server.sh "${SIDECAR_PID_FILE}"

MCP_PID_FILE  ?= $(BIN_DIR)/mcp.pid
MCP_LOG ?= $(BIN_DIR)/mcp.log
MCP_PORT ?= 3001
MCP_CONFIG_FILE ?= config/mcp_local.yaml

start-mcp: build-mcp ## Run the MCP server in background
	@echo "Running $(MCP_BINARY_NAME) on port $(MCP_PORT)..."
	@./scripts/start_mcp.sh "${MCP_PID_FILE}" "${BIN_DIR}/$(MCP_BINARY_NAME)" "${MCP_LOG}" "$(MCP_PORT)" "$(MCP_CONFIG_FILE)"

stop-mcp: ## Stop the MCP server
	-./scripts/stop_server.sh "${MCP_PID_FILE}"

# Use this to ignore any self-signed certificate errors
# NODE_TLS_REJECT_UNAUTHORIZED=0
start-inspector-mcp:
	npx @modelcontextprotocol/inspector

lint: ## Lint the code (runs go vet)
	@echo "Linting code..."
	@go vet ./...
	@echo "Lint complete"

fmt: ## Format the code with go fmt
	@echo "Formatting code with go fmt..."
	@go fmt ./...
	@echo "Format complete"

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...
	@echo "Vet complete"

test: ## Run unit tests
	@echo "Running unit tests..."
	@bash -c 'set -o pipefail; go test -v ./internal/... ./cmd/... ./pkg/... | ${PWD}/scripts/grcat ${PWD}/.conf.go-test'
	@echo "Unit tests complete"

test-coverage: $(BIN_DIR) ## Run unit tests with coverage
	@echo "Running unit tests with coverage..."
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage.out -covermode=atomic ./internal/... ./cmd/... ./pkg/...
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage-init.out -covermode=atomic ./cmd/eval_runtime_init
	@go tool cover -html=$(BIN_DIR)/coverage.out -o $(BIN_DIR)/coverage.html
	@go tool cover -html=$(BIN_DIR)/coverage-init.out -o $(BIN_DIR)/coverage-init.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage.html and $(BIN_DIR)/coverage-init.html"

test-all: test test-fvt test-fvt-server ## Run all tests (unit + FVT)

test-help: ## Display Go test flags documentation
	@go help testflag

SERVER_URL ?= http://localhost:8080

## ------------------------------------------------------------------------------------------------
## FVT tests (Functional Verification Tests) using godog
## ------------------------------------------------------------------------------------------------

SERVER_URL ?= http://localhost:8080

FVT_TESTS ?= ./tests/features/...
FVT_OUTPUT ?= --godog.format=junit:${PWD}/$(BIN_DIR)/junit-fvt-report.xml,pretty
FVT_TAGS ?= --godog.tags=~@ignore && ~@mlflow && ~@cluster && ~@local_runtime
FVT_CONCURRENCY ?= 1

.PHONY: test-setup
test-setup: venv ## Set up Python test environment (venv + eval-hub-sdk adapter)
	@uv pip install "eval-hub-sdk[adapter]>=0.1.5"

test-fvt: $(BIN_DIR) test-setup ## Run FVT (Functional Verification Tests) using godog
	@echo "Running FVT tests..."
	@if [ -f $(VENV_DIR)/bin/activate ]; then . $(VENV_DIR)/bin/activate; else . $(VENV_DIR)/Scripts/activate; fi && bash -c 'set -o pipefail; go test ${FVT_TESTS} ${FVT_OUTPUT} "${FVT_TAGS}" -v -race | ${PWD}/scripts/grcat ${PWD}/.conf.go-integration-test'

test-fvt-server: start-service ## Run FVT tests using godog against a running server
	@SERVER_URL="${SERVER_URL}" make test-fvt; status=$$?; make stop-service; exit $$status

test-fvt-coverage: $(BIN_DIR)## Run integration (FVT) tests with coverage
	@echo "Running integration (FVT) tests with coverage..."
	@go test ${FVT_TESTS} ${FVT_OUTPUT} "${FVT_TAGS}" -v -race -coverprofile=$(BIN_DIR)/coverage-fvt.out -covermode=atomic
	@go tool cover -html=$(BIN_DIR)/coverage-fvt.out -o $(BIN_DIR)/coverage-fvt.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage-fvt.html"

test-fvt-server-coverage: start-service-coverage ## Run FVT tests using godog against a running server with coverage
	@echo "Running FVT tests with coverage against a running server..."
	@GOCOVERDIR="${BIN_DIR}" SERVER_URL="${SERVER_URL}" make test-fvt; status=$$?; make stop-service; exit $$status
	go tool covdata textfmt -i ${BIN_DIR} -o ${BIN_DIR}/coverage-fvt.out
	@go tool cover -html=$(BIN_DIR)/coverage-fvt.out -o $(BIN_DIR)/coverage-fvt.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage-fvt.html"

test-all-coverage: test-coverage test-fvt-server-coverage ## Run all tests (unit + FVT) with coverage

fvt-report: ## Generate HTML report for FVT tests
	@echo "Generating FVT JSON report..."
	@GODOG_FORMAT=cucumber GODOG_OUTPUT="$${PWD}/cucumber-fvt.json" go test -v -race ./tests/features/...; status=$$?; \
	echo "Converting JSON report to HTML..."; \
	node -e "require('cucumber-html-reporter').generate({theme:'bootstrap',jsonFile:'cucumber-fvt.json',output:'cucumber-report.html'})" 2>&1; \
	report_status=$$?; \
	if [ $$report_status -ne 0 ]; then echo "Report generation failed (see output above)."; fi; \
	if [ -f cucumber-report.html ]; then echo "Report generated: cucumber-report.html"; else echo "Report not generated: cucumber-report.html"; fi; \
	exit $$status

${GOBIN}/go-cover-treemap:
	go install github.com/nikolaydubina/go-cover-treemap@latest

BIN_DIR_COVERAGE ?= $(BIN_DIR)/coverage

TREEMAP_OPTIONS ?= -w 1080 -h 360 -percent

coverage-treemap: ${GOBIN}/go-cover-treemap
	@echo "Generating coverage treemap for $(BIN_DIR)/coverage.out and $(BIN_DIR)/coverage-fvt.out"
	@rm -fr ${BIN_DIR_COVERAGE} && true
	@mkdir -p ${BIN_DIR_COVERAGE}
	go tool covdata merge -i=${BIN_DIR} -o=${BIN_DIR_COVERAGE}
	go tool covdata textfmt -i ${BIN_DIR_COVERAGE} -o ${BIN_DIR_COVERAGE}/coverage.out
	${GOBIN}/go-cover-treemap ${TREEMAP_OPTIONS} -coverprofile $(BIN_DIR_COVERAGE)/coverage.out > $(BIN_DIR_COVERAGE)/coverage.svg
	@echo "Coverage treemap generated: $(BIN_DIR_COVERAGE)/coverage.svg"

## ------------------------------------------------------------------------------------------------
## Dependencies
## ------------------------------------------------------------------------------------------------

install-deps: ## Install dependencies
	@command -v python3 >/dev/null 2>&1 || { echo "Error: Python 3 is required for make test (scripts/grcat). Install python3 and retry."; exit 1; }
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy
	@echo "Dependencies installed"

update-deps: ## Update all dependencies to latest versions
	@echo "Updating dependencies to latest versions..."
	@go get -t -u ./...
	@go mod tidy
	@echo "Dependencies updated"

get-deps: ## Get all dependencies
	@echo "Getting dependencies..."
	@go get ./...
	@go get -t ./...
	@echo "Dependencies updated"

## ------------------------------------------------------------------------------------------------
## Cross-compilation
## ------------------------------------------------------------------------------------------------

# Cross-compilation variables
CROSS_OUTPUT_SUFFIX = $(CROSS_GOOS)-$(CROSS_GOARCH)
CROSS_OUTPUT = bin/eval-hub-$(CROSS_OUTPUT_SUFFIX)$(if $(filter windows,$(CROSS_GOOS)),.exe,)

.PHONY: cross-compile
cross-compile: ## Build for specific platform: make cross-compile CROSS_GOOS=linux CROSS_GOARCH=amd64
	@echo "Cross-compiling for $(CROSS_GOOS)/$(CROSS_GOARCH)..."
	@mkdir -p $(BIN_DIR)
	GOOS=$(CROSS_GOOS) GOARCH=$(CROSS_GOARCH) CGO_ENABLED=0 go build -o $(CROSS_OUTPUT) -ldflags="-s -w ${LDFLAGS_X}" $(CMD_PATH)
	@echo "Built: $(CROSS_OUTPUT)"

.PHONY: build-all-platforms
build-all-platforms: ## Build for all supported platforms
	@$(MAKE) cross-compile CROSS_GOOS=linux CROSS_GOARCH=amd64
	@$(MAKE) cross-compile CROSS_GOOS=linux CROSS_GOARCH=arm64
	@$(MAKE) cross-compile CROSS_GOOS=darwin CROSS_GOARCH=amd64
	@$(MAKE) cross-compile CROSS_GOOS=darwin CROSS_GOARCH=arm64
	@$(MAKE) cross-compile CROSS_GOOS=windows CROSS_GOARCH=amd64

# MCP cross-compilation
MCP_CROSS_OUTPUT = bin/evalhub-mcp-$(CROSS_GOOS)-$(CROSS_GOARCH)$(if $(filter windows,$(CROSS_GOOS)),.exe,)

.PHONY: cross-compile-mcp
cross-compile-mcp: ## Build MCP for specific platform: make cross-compile-mcp CROSS_GOOS=linux CROSS_GOARCH=amd64
	@echo "Cross-compiling MCP for $(CROSS_GOOS)/$(CROSS_GOARCH)..."
	@mkdir -p $(BIN_DIR)
	GOOS=$(CROSS_GOOS) GOARCH=$(CROSS_GOARCH) CGO_ENABLED=0 go build -o $(MCP_CROSS_OUTPUT) -ldflags="-s -w ${LDFLAGS_X}" $(MCP_CMD_PATH)
	@echo "Built: $(MCP_CROSS_OUTPUT)"

.PHONY: build-all-platforms-mcp
build-all-platforms-mcp: ## Build MCP for all supported platforms
	@$(MAKE) cross-compile-mcp CROSS_GOOS=linux CROSS_GOARCH=amd64
	@$(MAKE) cross-compile-mcp CROSS_GOOS=linux CROSS_GOARCH=arm64
	@$(MAKE) cross-compile-mcp CROSS_GOOS=darwin CROSS_GOARCH=amd64
	@$(MAKE) cross-compile-mcp CROSS_GOOS=darwin CROSS_GOARCH=arm64
	@$(MAKE) cross-compile-mcp CROSS_GOOS=windows CROSS_GOARCH=amd64

# Python virtual environment - expects uv venv
VENV_DIR = .venv
VENV_PYTHON = $(VENV_DIR)/bin/python

.PHONY: venv
venv: ## Create Python virtual environment using uv
	@if [ ! -d "$(VENV_DIR)" ]; then \
		echo "Creating uv virtual environment..."; \
		uv venv $(VENV_DIR) --python 3.11; \
		echo "Virtual environment created at $(VENV_DIR)"; \
	else \
		echo "Virtual environment already exists at $(VENV_DIR)"; \
	fi

# Python wheel building - auto-detect platform based on CROSS_GOOS/CROSS_GOARCH (we re-use CROSS_OUTPUT_SUFFIX)
# Platform mappings as defined also in .github/workflows/publish-python-server.yml
ifeq ($(CROSS_OUTPUT_SUFFIX),linux-amd64)
    WHEEL_PLATFORM ?= manylinux_2_17_x86_64
    WHEEL_BINARY ?= eval-hub-linux-amd64
else ifeq ($(CROSS_OUTPUT_SUFFIX),linux-arm64)
    WHEEL_PLATFORM ?= manylinux_2_17_aarch64
    WHEEL_BINARY ?= eval-hub-linux-arm64
else ifeq ($(CROSS_OUTPUT_SUFFIX),darwin-amd64)
    WHEEL_PLATFORM ?= macosx_10_9_x86_64
    WHEEL_BINARY ?= eval-hub-darwin-amd64
else ifeq ($(CROSS_OUTPUT_SUFFIX),darwin-arm64)
    WHEEL_PLATFORM ?= macosx_11_0_arm64
    WHEEL_BINARY ?= eval-hub-darwin-arm64
else ifeq ($(CROSS_OUTPUT_SUFFIX),windows-amd64)
    WHEEL_PLATFORM ?= win_amd64
    WHEEL_BINARY ?= eval-hub-windows-amd64
else
    # Fallback to macOS ARM (M-chips) if platform not recognized
    WHEEL_PLATFORM ?= macosx_11_0_arm64
    WHEEL_BINARY ?= eval-hub-darwin-arm64
endif

.PHONY: install-wheel-tools
install-wheel-tools: venv ## Install Python wheel build tools using uv
	@echo "Installing wheel build tools via uv..."
	@uv pip install build wheel setuptools

.PHONY: test-python-server
test-python-server: ## Run python-server tests (you probably need `build-wheel` first, we use this target as-is in GHA/ci/cd)
	@echo "Running python-server tests..."
	@cd python-server && uv run --extra dev pytest

.PHONY: clean-wheels
clean-wheels: ## Clean Python wheel build artifacts
	@echo "Cleaning wheel build artifacts..."
	@rm -rf python-server/dist/
	@rm -rf python-server/build/
	@rm -rf python-server/*.egg-info
	@find python-server/evalhub_server/binaries/ -type f ! -name '.gitkeep' -delete
	@rm -f python-server/VERSION

.PHONY: build-wheel
build-wheel: ## Build Python wheel: make build-wheel WHEEL_PLATFORM=manylinux_2_17_x86_64 WHEEL_BINARY=eval-hub-linux-amd64
	@if [ "$${GITHUB_ACTIONS}" != "true" ]; then \
		$(MAKE) cross-compile; \
		echo "Copying binary $(WHEEL_PLATFORM) $(WHEEL_BINARY)"; \
		mkdir -p python-server/evalhub_server/binaries/; \
		find python-server/evalhub_server/binaries/ -type f ! -name '.gitkeep' -delete; \
		cp bin/$(WHEEL_BINARY)* python-server/evalhub_server/binaries/; \
	else \
		echo "Skipping copy (GITHUB_ACTIONS): binary provided by actions/download-artifact"; \
	fi
	@find python-server/evalhub_server/binaries/ -type f ! -name '.gitkeep' -exec chmod +x {} +
	@echo "Building wheel for $(WHEEL_PLATFORM) with binary $(WHEEL_BINARY)..."
	@rm -rf python-server/build/
	@cp VERSION python-server/VERSION
	@if [ -n "$(DEV_SUFFIX)" ]; then \
		BASE=$$(tr -d '\n' < python-server/VERSION); \
		echo "$${BASE}.$(DEV_SUFFIX)" > python-server/VERSION; \
		echo "Python package version: $${BASE}.$(DEV_SUFFIX)"; \
	fi
	WHEEL_PLATFORM=$(WHEEL_PLATFORM) uv build --wheel python-server

.PHONY: build-all-wheels
build-all-wheels: clean-wheels build-all-platforms ## Build all Python wheels for all platforms
	@$(MAKE) build-wheel WHEEL_PLATFORM=manylinux_2_17_x86_64 WHEEL_BINARY=eval-hub-linux-amd64
	@$(MAKE) build-wheel WHEEL_PLATFORM=manylinux_2_17_aarch64 WHEEL_BINARY=eval-hub-linux-arm64
	@$(MAKE) build-wheel WHEEL_PLATFORM=macosx_10_9_x86_64 WHEEL_BINARY=eval-hub-darwin-amd64
	@$(MAKE) build-wheel WHEEL_PLATFORM=macosx_11_0_arm64 WHEEL_BINARY=eval-hub-darwin-arm64
	@$(MAKE) build-wheel WHEEL_PLATFORM=win_amd64 WHEEL_BINARY=eval-hub-windows-amd64

.PHONY: cls
cls:
	printf "\33c\e[3J"

## Targets for the API documentation

.PHONY: generate-public-docs verify-api-docs generate-ignore-file

REDOCLY_CLI ?= ${PWD}/node_modules/.bin/redocly

${REDOCLY_CLI}:
	npm i @redocly/cli

clean-docs:
	rm -f docs/openapi.yaml docs/openapi.json docs/openapi-internal.yaml docs/openapi-internal.json docs/*.html

generate-public-docs: ${REDOCLY_CLI}
	${REDOCLY_CLI} bundle external@latest --output docs/openapi.yaml --remove-unused-components
	${REDOCLY_CLI} bundle external@latest --ext json --output docs/openapi.json
	${REDOCLY_CLI} bundle internal@latest --output docs/openapi-internal.yaml --remove-unused-components
	${REDOCLY_CLI} bundle internal@latest --ext json --output docs/openapi-internal.json
	${REDOCLY_CLI} build-docs docs/openapi.json --output=docs/index-public.html
	${REDOCLY_CLI} build-docs docs/openapi-internal.json --output=docs/index-private.html
	cp docs/index-public.html docs/index.html

verify-api-docs: ${REDOCLY_CLI}
	${REDOCLY_CLI} lint
	@echo "Tip: open docs/openapi.yaml in Swagger Editor (such as https://editor.swagger.io/) to automatically inspect the rendered spec or open the file docs/index.html."

generate-ignore-file: ${REDOCLY_CLI}
	${REDOCLY_CLI} lint --generate-ignore-file ./docs/src/openapi.yaml

check-unused-components:
	./docs/scripts/check_unused_components.sh

documentation: check-unused-components generate-public-docs verify-api-docs

update-redocly-cli:
	rm -f package-lock.json
	npm install @redocly/cli@latest

# Local image build (same Containerfile and BUILD_DATE as .github/workflows/ci.yml docker-build-push; pass GIT_HASH for embedded evalhub-mcp metadata).
DOCKER_IMAGE_LOCAL ?= eval-hub:local
DOCKER_BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# Container build tool: podman or docker
DOCKER ?= podman

docker-image-local: ## Build the eval-hub Docker image locally from Containerfile
	$(DOCKER) build -f Containerfile \
		--build-arg "BUILD_DATE=$(DOCKER_BUILD_DATE)" \
		--build-arg "GIT_HASH=$(GIT_HASH)" \
		-t "$(DOCKER_IMAGE_LOCAL)" .

docker-mcp-version: ## Run evalhub-mcp --version in the local Docker image (build with docker-image-local first)
	$(DOCKER) run --rm "$(DOCKER_IMAGE_LOCAL)" /app/evalhub-mcp --version

## ------------------------------------------------------------------------------------------------
## Cross-Platform Build Tests (RHOAIENG-60352)
## See tests/cross-platform-build/TEST_PLAN.md for full test plan.
## ------------------------------------------------------------------------------------------------

MCP_TEST_IMAGE ?= evalhub-mcp-test:latest
MCP_TEST_CONTAINER ?= evalhub-mcp-test
MCP_TEST_PORT ?= 3001

MCP_PLATFORMS = linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64

test-mcp-build-all: ## Build all 5 MCP platform binaries and verify they exist
	@echo "=== test-mcp-build-all ==="
	@$(MAKE) build-all-platforms-mcp
	@echo "Verifying all platform binaries exist..."
	@fail=0; \
	for p in $(MCP_PLATFORMS); do \
		bin="bin/evalhub-mcp-$$p"; \
		if [ "$$p" = "windows-amd64" ]; then bin="$${bin}.exe"; fi; \
		if [ ! -f "$$bin" ]; then echo "FAIL: missing $$bin"; fail=1; else echo "OK: $$bin"; fi; \
	done; \
	if [ $$fail -ne 0 ]; then echo "FAIL: not all binaries present"; exit 1; fi
	@echo "PASS: all 5 platform binaries built successfully"

test-mcp-binary-info: ## Verify each binary has the correct file type and architecture
	@echo "=== test-mcp-binary-info ==="
	@fail=0; \
	check() { \
		if echo "$$2" | grep -qE "$$3"; then echo "OK: $$1 -> $$3"; else echo "FAIL: $$1 expected '$$3', got '$$2'"; fail=1; fi; \
	}; \
	info=$$(file bin/evalhub-mcp-linux-amd64 2>/dev/null); \
	echo "$$info" | grep -qE "ELF 64-bit.*x86-64" || { echo "FAIL: linux-amd64: $$info"; fail=1; }; \
	echo "$$info" | grep -qE "ELF 64-bit.*x86-64" && echo "OK: linux-amd64 is ELF 64-bit x86-64"; \
	info=$$(file bin/evalhub-mcp-linux-arm64 2>/dev/null); \
	echo "$$info" | grep -qiE "ELF 64-bit.*(aarch64|ARM aarch64)" || { echo "FAIL: linux-arm64: $$info"; fail=1; }; \
	echo "$$info" | grep -qiE "ELF 64-bit.*(aarch64|ARM aarch64)" && echo "OK: linux-arm64 is ELF 64-bit aarch64"; \
	info=$$(file bin/evalhub-mcp-darwin-amd64 2>/dev/null); \
	echo "$$info" | grep -qiE "Mach-O.*x86_64" || { echo "FAIL: darwin-amd64: $$info"; fail=1; }; \
	echo "$$info" | grep -qiE "Mach-O.*x86_64" && echo "OK: darwin-amd64 is Mach-O x86_64"; \
	info=$$(file bin/evalhub-mcp-darwin-arm64 2>/dev/null); \
	echo "$$info" | grep -qiE "Mach-O.*arm64" || { echo "FAIL: darwin-arm64: $$info"; fail=1; }; \
	echo "$$info" | grep -qiE "Mach-O.*arm64" && echo "OK: darwin-arm64 is Mach-O arm64"; \
	info=$$(file bin/evalhub-mcp-windows-amd64.exe 2>/dev/null); \
	echo "$$info" | grep -qiE "PE32\+.*x86-64" || { echo "FAIL: windows-amd64: $$info"; fail=1; }; \
	echo "$$info" | grep -qiE "PE32\+.*x86-64" && echo "OK: windows-amd64 is PE32+ x86-64"; \
	if [ $$fail -ne 0 ]; then exit 1; fi
	@echo "PASS: all binaries have correct file type and architecture"

test-mcp-binary-naming: ## Verify binaries follow the evalhub-mcp-{OS}-{ARCH} naming convention
	@echo "=== test-mcp-binary-naming ==="
	@count=$$(ls -1 bin/evalhub-mcp-* 2>/dev/null | wc -l); \
	if [ "$$count" -ne 5 ]; then echo "FAIL: expected 5 platform binaries, found $$count"; exit 1; fi
	@fail=0; \
	for p in $(MCP_PLATFORMS); do \
		bin="bin/evalhub-mcp-$$p"; \
		if [ "$$p" = "windows-amd64" ]; then bin="$${bin}.exe"; fi; \
		if [ ! -f "$$bin" ]; then echo "FAIL: missing $$bin"; fail=1; else echo "OK: $$bin matches naming convention"; fi; \
	done; \
	if [ $$fail -ne 0 ]; then exit 1; fi
	@echo "PASS: all binaries follow naming convention"

test-mcp-version: ## Verify --version outputs correct build metadata
	@echo "=== test-mcp-version ==="
	@echo "Building native MCP binary for version test..."
	@$(MAKE) build-mcp
	@expected_version=$$(cat VERSION); \
	output=$$(bin/evalhub-mcp --version 2>&1); \
	echo "Version output: $$output"; \
	fail=0; \
	echo "$$output" | grep -q "evalhub-mcp version" || { echo "FAIL: missing 'evalhub-mcp version' prefix"; fail=1; }; \
	echo "$$output" | grep -q "$$expected_version" || { echo "FAIL: version '$$expected_version' not found in output"; fail=1; }; \
	echo "$$output" | grep -q "build:" || { echo "FAIL: missing build info"; fail=1; }; \
	echo "$$output" | grep -q "commit:" || { echo "FAIL: missing commit hash"; fail=1; }; \
	echo "$$output" | grep -q "built:" || { echo "FAIL: missing build date"; fail=1; }; \
	if [ $$fail -ne 0 ]; then exit 1; fi
	@echo "PASS: --version outputs correct build metadata"

test-mcp-no-runtime-deps: ## Verify Linux binaries are statically linked
	@echo "=== test-mcp-no-runtime-deps ==="
	@fail=0; \
	for arch in amd64 arm64; do \
		bin="bin/evalhub-mcp-linux-$$arch"; \
		if [ ! -f "$$bin" ]; then echo "SKIP: $$bin not found (run test-mcp-build-all first)"; continue; fi; \
		info=$$(file "$$bin"); \
		echo "$$info" | grep -q "statically linked" || { echo "FAIL: $$bin is not statically linked: $$info"; fail=1; }; \
		echo "$$info" | grep -q "statically linked" && echo "OK: $$bin is statically linked"; \
	done; \
	if [ $$fail -ne 0 ]; then exit 1; fi
	@echo "PASS: Linux binaries are statically linked (no external runtime deps)"

test-mcp-container-build: ## Build and verify container image
	@echo "=== test-mcp-container-build ==="
	$(DOCKER) build -f Containerfile \
		--build-arg "BUILD_DATE=$(DOCKER_BUILD_DATE)" \
		--build-arg "GIT_HASH=$(GIT_HASH)" \
		-t "$(MCP_TEST_IMAGE)" .
	@echo "Verifying evalhub-mcp binary exists in image..."
	@$(DOCKER) run --rm "$(MCP_TEST_IMAGE)" test -x /app/evalhub-mcp || { echo "FAIL: /app/evalhub-mcp not found or not executable"; exit 1; }
	@echo "PASS: container image builds successfully and contains evalhub-mcp"

test-mcp-container-http: ## Start container in HTTP mode and test MCP initialize
	@echo "=== test-mcp-container-http ==="
	-@$(DOCKER) rm -f $(MCP_TEST_CONTAINER) 2>/dev/null || true
	@echo "Starting container in HTTP mode on port $(MCP_TEST_PORT)..."
	@$(DOCKER) run -d --name $(MCP_TEST_CONTAINER) \
		-p $(MCP_TEST_PORT):3001 \
		"$(MCP_TEST_IMAGE)" /app/evalhub-mcp --transport http --host 0.0.0.0 --port 3001
	@echo "Waiting for server to start..."
	@sleep 3
	@echo "Sending MCP initialize request..."
	@response=$$(curl -sf -X POST http://localhost:$(MCP_TEST_PORT)/mcp \
		-H "Content-Type: application/json" \
		-d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}' 2>&1) || \
		{ echo "FAIL: MCP initialize request failed"; $(DOCKER) logs $(MCP_TEST_CONTAINER); $(DOCKER) rm -f $(MCP_TEST_CONTAINER); exit 1; }; \
	echo "Response: $$response"; \
	echo "$$response" | grep -q "serverInfo" || \
		{ echo "FAIL: response missing serverInfo"; $(DOCKER) rm -f $(MCP_TEST_CONTAINER); exit 1; }
	@$(DOCKER) rm -f $(MCP_TEST_CONTAINER)
	@echo "PASS: container starts in HTTP mode and responds to MCP initialize"

test-mcp-checksums: ## Generate SHA256 checksums and verify they match binaries
	@echo "=== test-mcp-checksums ==="
	@if command -v sha256sum >/dev/null 2>&1; then \
		CMD="sha256sum"; \
	else \
		CMD="shasum -a 256"; \
	fi; \
	cd bin && $$CMD evalhub-mcp-* > checksums-sha256.txt
	@echo "Generated checksums:"
	@cat bin/checksums-sha256.txt
	@entry_count=$$(wc -l < bin/checksums-sha256.txt); \
	if [ "$$entry_count" -lt 5 ]; then echo "FAIL: expected at least 5 checksum entries, found $$entry_count"; exit 1; fi
	@if command -v sha256sum >/dev/null 2>&1; then \
		cd bin && sha256sum --check checksums-sha256.txt; \
	else \
		cd bin && shasum -a 256 -c checksums-sha256.txt; \
	fi || { echo "FAIL: checksum verification failed"; exit 1; }
	@echo "PASS: SHA256 checksums generated and verified for all binaries"

test-mcp-formula-syntax: ## Validate Homebrew formula is syntactically valid Ruby
	@echo "=== test-mcp-formula-syntax ==="
	@if command -v ruby >/dev/null 2>&1; then \
		ruby -c formula/evalhub-mcp.rb || { echo "FAIL: formula syntax error"; exit 1; }; \
	else \
		echo "SKIP: ruby not available, checking formula content directly"; \
	fi
	@echo "Checking formula references all platforms..."
	@fail=0; \
	grep -q "darwin-amd64" formula/evalhub-mcp.rb || { echo "FAIL: missing darwin-amd64"; fail=1; }; \
	grep -q "darwin-arm64" formula/evalhub-mcp.rb || { echo "FAIL: missing darwin-arm64"; fail=1; }; \
	grep -q "linux-amd64" formula/evalhub-mcp.rb || { echo "FAIL: missing linux-amd64"; fail=1; }; \
	grep -q "linux-arm64" formula/evalhub-mcp.rb || { echo "FAIL: missing linux-arm64"; fail=1; }; \
	grep -q "\-\-version" formula/evalhub-mcp.rb || { echo "FAIL: formula test block missing --version check"; fail=1; }; \
	if [ $$fail -ne 0 ]; then exit 1; fi
	@echo "PASS: Homebrew formula is valid and references all platforms"

## ------------------------------------------------------------------------------------------------
## Homebrew Integration Tests (macOS/Linux only)
## Run on a Mac laptop: make test-mcp-brew-install test-mcp-brew-test test-mcp-brew-uninstall
## ------------------------------------------------------------------------------------------------

BREW_TAP ?= eval-hub/evalhub

test-mcp-native-smoke: ## Build and run --version on native-platform MCP binary
	@echo "=== test-mcp-native-smoke ==="
	@native_os=$$(go env GOOS); native_arch=$$(go env GOARCH); \
	echo "Building evalhub-mcp for native platform $$native_os/$$native_arch..."; \
	$(MAKE) cross-compile-mcp CROSS_GOOS=$$native_os CROSS_GOARCH=$$native_arch; \
	bin="bin/evalhub-mcp-$${native_os}-$${native_arch}"; \
	if [ "$$native_os" = "windows" ]; then bin="$${bin}.exe"; fi; \
	chmod +x "$$bin"; \
	echo "Running $$bin --version..."; \
	output=$$("$$bin" --version 2>&1); \
	echo "$$output"; \
	echo "$$output" | grep -q "evalhub-mcp version" || { echo "FAIL: unexpected --version output"; exit 1; }
	@echo "PASS: native binary runs and reports version correctly"

test-mcp-brew-install: ## Install evalhub-mcp via local Homebrew tap (macOS/Linux)
	@echo "=== test-mcp-brew-install ==="
	@command -v brew >/dev/null 2>&1 || { echo "FAIL: brew not found — install Homebrew first"; exit 1; }
	@native_os=$$(go env GOOS); native_arch=$$(go env GOARCH); \
	echo "Building binary for $$native_os/$$native_arch..."; \
	$(MAKE) cross-compile-mcp CROSS_GOOS=$$native_os CROSS_GOARCH=$$native_arch
	@echo "Patching formula with local binary..."
	@native_os=$$(go env GOOS); native_arch=$$(go env GOARCH); \
	bin="bin/evalhub-mcp-$${native_os}-$${native_arch}"; \
	if command -v shasum >/dev/null 2>&1; then \
		sha=$$(shasum -a 256 "$$bin" | awk '{print $$1}'); \
	else \
		sha=$$(sha256sum "$$bin" | awk '{print $$1}'); \
	fi; \
	echo "SHA256: $$sha"; \
	abs_bin=$$(cd "$$(dirname $$bin)" && pwd)/$$(basename $$bin); \
	sed \
		-e 's|version ".*"|version "$(FULL_BUILD_NUMBER)"|' \
		-e 's|sha256 "PLACEHOLDER"|sha256 "'$$sha'"|g' \
		-e 's|url "https://.*"|url "file://'"$$abs_bin"'"|g' \
		formula/evalhub-mcp.rb > formula/evalhub-mcp-local.rb; \
	echo "Patched formula written to formula/evalhub-mcp-local.rb"
	@echo "Installing local tap..."
	@tap_dir=$$(brew --repository)/Library/Taps/eval-hub/homebrew-evalhub; \
	mkdir -p "$$tap_dir"; \
	cp formula/evalhub-mcp-local.rb "$$tap_dir/evalhub-mcp.rb"
	@brew install --formula $(BREW_TAP)/evalhub-mcp || brew reinstall --formula $(BREW_TAP)/evalhub-mcp
	@echo "Verifying installation..."
	@which evalhub-mcp || { echo "FAIL: evalhub-mcp not found in PATH after install"; exit 1; }
	@evalhub-mcp --version
	@echo "PASS: evalhub-mcp installed via Homebrew local tap"

test-mcp-brew-test: ## Run brew test on the installed evalhub-mcp formula
	@echo "=== test-mcp-brew-test ==="
	@command -v brew >/dev/null 2>&1 || { echo "FAIL: brew not found"; exit 1; }
	@brew test $(BREW_TAP)/evalhub-mcp || { echo "FAIL: brew test failed"; exit 1; }
	@echo "PASS: brew test evalhub-mcp passed"

test-mcp-brew-uninstall: ## Uninstall evalhub-mcp and remove local tap
	@echo "=== test-mcp-brew-uninstall ==="
	@command -v brew >/dev/null 2>&1 || { echo "SKIP: brew not found"; exit 0; }
	-@brew uninstall $(BREW_TAP)/evalhub-mcp 2>/dev/null
	-@brew untap $(BREW_TAP) 2>/dev/null
	-@rm -f formula/evalhub-mcp-local.rb
	@echo "PASS: evalhub-mcp uninstalled and local tap removed"

test-mcp-cross-platform: test-mcp-build-all test-mcp-binary-info test-mcp-binary-naming test-mcp-version test-mcp-no-runtime-deps test-mcp-container-build test-mcp-container-http test-mcp-checksums test-mcp-formula-syntax ## Run all cross-platform build tests (CI-safe, no brew)
	@echo ""
	@echo "========================================"
	@echo "  All cross-platform build tests PASSED"
	@echo "========================================"

test-mcp-e2e: start-service ## Run end-to-end MCP tests
	@echo "Running end-to-end MCP tests..."
	@./tests/mcp/scripts/part1_stdio_transport.sh && \
	./tests/mcp/scripts/part2_http_transport.sh && \
	./tests/mcp/scripts/part3_error_scenarios.sh && \
	./tests/mcp/scripts/part4_e2e_workflow.sh; \
	status=$$?; \
	echo "End-to-end MCP tests complete"; \
	$(MAKE) stop-service; \
	exit $$status

test-mcp: test-mcp-cross-platform test-mcp-e2e

## ------------------------------------------------------------------------------------------------
## VS Code/Cursor MCP Integration Tests
## ------------------------------------------------------------------------------------------------

MCP_VSCODE_TEST_DIR = tests/mcp/vscode/test-scripts

test-mcp-vscode: start-service build-mcp ## Run VS Code/Cursor MCP test scripts (all suites, stdio + HTTP)
	@test -f $(MCP_VSCODE_TEST_DIR)/test.env || { \
		echo "ERROR: $(MCP_VSCODE_TEST_DIR)/test.env not found."; \
		echo "       Copy test.env.example to test.env and configure it."; \
		exit 1; \
	}
	@echo "Running VS Code/Cursor MCP integration tests..."
	@$(MAKE) stop-mcp
	@$(MAKE) start-mcp
	@EVALHUB_MCP_BIN="$(CURDIR)/$(BIN_DIR)/$(MCP_BINARY_NAME)" \
		EVALHUB_BASE_URL="http://localhost:8080" \
		EVALHUB_TOKEN="token" \
		EVALHUB_TENANT="tenant" \
		./$(MCP_VSCODE_TEST_DIR)/run_tests.sh http; \
	status=$$?; \
	$(MAKE) stop-mcp stop-service; \
	exit $$status
