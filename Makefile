.PHONY: help autoupdate-precommit pre-commit clean build build-coverage build-service build-init build-sidecar build-all-platforms start-service stop-service start-sidecar stop-sidecar lint test test-fvt-server test-all test-coverage test-fvt-coverage test-fvt-server-coverage test-all-coverage install-deps update-deps get-deps fmt vet update-deps generate-public-docs verify-api-docs generate-ignore-file documentation check-unused-components fvt-report

# Variables
BINARY_NAME = eval-hub
CMD_PATH = ./cmd/eval_hub
INIT_BINARY_NAME = eval-runtime-init
INIT_CMD_PATH = ./cmd/eval_runtime_init
SIDECAR_BINARY_NAME = eval-runtime-sidecar
SIDECAR_CMD_PATH = ./cmd/eval_runtime_sidecar
BIN_DIR = bin
PORT ?= 8080

# Default target
.DEFAULT_GOAL := help

# Auto-detect platform for cross-compilation and wheel building
# Uses Go's native platform detection - override by setting CROSS_GOOS/CROSS_GOARCH env vars if needed.
CROSS_GOOS ?= $(shell go env GOOS)
CROSS_GOARCH ?= $(shell go env GOARCH)

DATE ?= $(shell date +%FT%T%z)

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
	@echo "Clean complete"

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

BUILD_PACKAGE ?= main
FULL_BUILD_NUMBER ?= $(shell cat VERSION)
LDFLAGS_X = -X "${BUILD_PACKAGE}.Build=${FULL_BUILD_NUMBER}" -X "${BUILD_PACKAGE}.BuildDate=$(DATE)"
LDFLAGS = -buildmode=exe ${LDFLAGS_X}

build-service: $(BIN_DIR) ## Build the service binary
	@echo "Building $(BINARY_NAME) with ${LDFLAGS}"
	@go build -race -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

build-init: $(BIN_DIR) ## Build the eval-runtime-init binary only
	@echo "Building $(INIT_BINARY_NAME) with ${LDFLAGS}"
	@go build -race -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(INIT_BINARY_NAME) $(INIT_CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(INIT_BINARY_NAME)"

build: build-service build-init build-sidecar ## Build the binaries

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

start-service: ${SERVER_PID_FILE} build-service ## Run the application in background
	@echo "Running $(BINARY_NAME) on port $(PORT)..."
	@./scripts/start_server.sh "${SERVER_PID_FILE}" "${BIN_DIR}/$(BINARY_NAME)" "${SERVICE_LOG}" ${PORT} ""

start-service-coverage: ${SERVER_PID_FILE} build-coverage ## Run the application in background
	@echo "Running $(BINARY_NAME)-cov on port $(PORT)..."
	@./scripts/start_server.sh "${SERVER_PID_FILE}" "${BIN_DIR}/$(BINARY_NAME)-cov" "${SERVICE_LOG}" ${PORT} "${BIN_DIR}"

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

start-sidecar: build-sidecar ## Run the sidecar in background (port $(SIDECAR_PORT), config from $(SIDECAR_CONFIG_DIR))
	@rm -f "${SIDECAR_PID_FILE}" && true
	@echo "Running $(SIDECAR_BINARY_NAME) on port $(SIDECAR_PORT) (config: $(SIDECAR_CONFIG_DIR))..."
	@SIDECAR_PORT="$(SIDECAR_PORT)" ./scripts/start_sidecar.sh "${SIDECAR_PID_FILE}" "${BIN_DIR}/$(SIDECAR_BINARY_NAME)" "${SIDECAR_LOG}" "$(SIDECAR_PORT)" "$(SIDECAR_CONFIG_DIR)"

stop-sidecar: ## Stop the sidecar
	-./scripts/stop_server.sh "${SIDECAR_PID_FILE}"

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
	@bash -c 'set -o pipefail; go test -v ./auth/... ./internal/... ./cmd/... | ${PWD}/scripts/grcat ${PWD}/.conf.go-test'
	@echo "Unit tests complete"

test-coverage: $(BIN_DIR) ## Run unit tests with coverage
	@echo "Running unit tests with coverage..."
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage.out -covermode=atomic ./internal/... ./cmd/...
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage-init.out -covermode=atomic ./cmd/eval_runtime_init
	@go tool cover -html=$(BIN_DIR)/coverage.out -o $(BIN_DIR)/coverage.html
	@go tool cover -html=$(BIN_DIR)/coverage-init.out -o $(BIN_DIR)/coverage-init.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage.html and $(BIN_DIR)/coverage-init.html"

test-all: test test-fvt test-fvt-server ## Run all tests (unit + FVT)

SERVER_URL ?= http://localhost:8080

## ------------------------------------------------------------------------------------------------
## FVT tests (Functional Verification Tests) using godog
## ------------------------------------------------------------------------------------------------

SERVER_URL ?= http://localhost:8080

FVT_TESTS ?= ./tests/features/...
FVT_OUTPUT ?= --godog.format=junit:${PWD}/$(BIN_DIR)/junit-fvt-report.xml,pretty

test-fvt: $(BIN_DIR) ## Run FVT (Functional Verification Tests) using godog
	@echo "Running FVT tests..."
	@bash -c 'set -o pipefail; go test ${FVT_TESTS} ${FVT_OUTPUT} -v -race | ${PWD}/scripts/grcat ${PWD}/.conf.go-integration-test'

test-fvt-server: start-service ## Run FVT tests using godog against a running server
	@SERVER_URL="${SERVER_URL}" make test-fvt; status=$$?; make stop-service; exit $$status

test-fvt-coverage: $(BIN_DIR)## Run integration (FVT) tests with coverage
	@echo "Running integration (FVT) tests with coverage..."
	@go test ${FVT_TESTS} ${FVT_OUTPUT} -v -race -coverprofile=$(BIN_DIR)/coverage-fvt.out -covermode=atomic
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

# Python virtual environment - expects uv venv
VENV_DIR = .venv
VENV_PYTHON = $(VENV_DIR)/bin/python

.PHONY: venv
venv: ## Create Python virtual environment using uv
	@if [ ! -d "$(VENV_DIR)" ]; then \
		echo "Creating uv virtual environment..."; \
		uv venv $(VENV_DIR); \
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
		echo "Downloading binary $(WHEEL_PLATFORM) $(WHEEL_BINARY)"; \
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
	@echo "Tip: open docs/openapi.yaml in Swagger Editor (such as https://editor.swagger.io/) to automatically inspect the rendered spec."

generate-ignore-file: ${REDOCLY_CLI}
	${REDOCLY_CLI} lint --generate-ignore-file ./docs/src/openapi.yaml

check-unused-components:
	./docs/scripts/check_unused_components.sh

documentation: check-unused-components generate-public-docs verify-api-docs

update-redocly-cli:
	rm -f package-lock.json
	npm install @redocly/cli@latest
