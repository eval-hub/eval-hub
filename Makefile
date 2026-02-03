.PHONY: help autoupdate-precommit pre-commit clean build build-coverage start-service stop-service lint test test-fvt-server test-all test-coverage test-fvt-coverage test-fvt-server-coverage test-all-coverage install-deps update-deps get-deps fmt vet update-deps generate-public-docs verify-api-docs generate-ignore-file documentation

# Variables
BINARY_NAME = eval-hub
CMD_PATH = ./cmd/eval_hub
BIN_DIR = bin
PORT ?= 8080

# Default target
.DEFAULT_GOAL := help

UNAME := $(shell uname)

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
FULL_BUILD_NUMBER ?= 0.0.1
LDFLAGS_X = -X "${BUILD_PACKAGE}.Build=${FULL_BUILD_NUMBER}" -X "${BUILD_PACKAGE}.BuildDate=$(DATE)"
LDFLAGS = -buildmode=exe ${LDFLAGS_X}

build: $(BIN_DIR) ## Build the binary
	@echo "Building $(BINARY_NAME) with ${LDFLAGS}"
	@go build -race -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

build-coverage: $(BIN_DIR) ## Build the binary with coverage
	@echo "Building $(BINARY_NAME)-cov with -cover -covermode=atomic -ldflags ${LDFLAGS} "
	@go build -race -cover -covermode=atomic -coverpkg=./... -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(BINARY_NAME)-cov $(CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)-cov"

SERVER_PID_FILE ?= $(BIN_DIR)/pid

${SERVER_PID_FILE}:
	rm -f "${SERVER_PID_FILE}" && true

SERVICE_LOG ?= $(BIN_DIR)/service.log

start-service: ${SERVER_PID_FILE} build ## Run the application in background
	@echo "Running $(BINARY_NAME) on port $(PORT)..."
	@./scripts/start_server.sh "${SERVER_PID_FILE}" "${BIN_DIR}/$(BINARY_NAME)" "${SERVICE_LOG}" ${PORT} ""

start-service-coverage: ${SERVER_PID_FILE} build-coverage ## Run the application in background
	@echo "Running $(BINARY_NAME)-cov on port $(PORT)..."
	@./scripts/start_server.sh "${SERVER_PID_FILE}" "${BIN_DIR}/$(BINARY_NAME)-cov" "${SERVICE_LOG}" ${PORT} "${BIN_DIR}"

stop-service:
	-./scripts/stop_server.sh "${SERVER_PID_FILE}"
	! grep -i -F panic "${SERVICE_LOG}"

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
	@go test -v ./internal/...

test-fvt: $(BIN_DIR) ## Run FVT (Functional Verification Tests) using godog
	@echo "Running FVT tests..."
	@go test -v -race ./tests/features/...

test-all: test test-fvt ## Run all tests (unit + FVT)

SERVER_URL ?= http://localhost:8080

test-fvt-server: start-service ## Run FVT tests using godog against a running server
	@SERVER_URL="${SERVER_URL}" make test-fvt; status=$$?; make stop-service; exit $$status

test-coverage: $(BIN_DIR) ## Run unit tests with coverage
	@echo "Running unit tests with coverage..."
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage.out -covermode=atomic ./internal/... ./cmd/...
	@go tool cover -html=$(BIN_DIR)/coverage.out -o $(BIN_DIR)/coverage.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage.html"

test-fvt-coverage: $(BIN_DIR)## Run integration (FVT) tests with coverage
	@echo "Running integration (FVT) tests with coverage..."
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage-fvt.out -covermode=atomic ./tests/features/...
	@go tool cover -html=$(BIN_DIR)/coverage-fvt.out -o $(BIN_DIR)/coverage-fvt.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage-fvt.html"

test-fvt-server-coverage: start-service-coverage ## Run FVT tests using godog against a running server with coverage
	@echo "Running FVT tests with coverage against a running server..."
	@GOCOVERDIR="${BIN_DIR}" SERVER_URL="${SERVER_URL}" make test-fvt; status=$$?; make stop-service; exit $$status
	go tool covdata textfmt -i ${BIN_DIR} -o ${BIN_DIR}/coverage-fvt.out

test-all-coverage: test-coverage test-fvt-server-coverage ## Run all tests (unit + FVT) with coverage

install-deps: ## Install dependencies
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

POSTGRES_VERSION ?= 18

ifeq (${UNAME}, Darwin)
install-postgres:
	brew install postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
install-postgres:
	sudo apt-get install postgresql
else
install-postgres:
	echo "Unsupported platform: ${UNAME}"
	exit 1
endif

ifeq (${UNAME}, Darwin)
start-postgres:
	brew services start postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
start-postgres:
	sudo systemctl start postgresql
endif

ifeq (${UNAME}, Darwin)
stop-postgres:
	brew services stop postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
stop-postgres:
	sudo systemctl stop postgresql
endif

create-database:
	sudo -u postgres createdb eval_hub

create-user:
	sudo -u postgres createuser -s -d -r eval_hub

grant-permissions:
	sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE eval_hub TO eval_hub;"

# Cross-compilation with parameters - default to macOS ARM
CROSS_GOOS ?= darwin
CROSS_GOARCH ?= arm64
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

# Python wheel building with parameters - default to macOS ARM
WHEEL_PLATFORM ?= macosx_11_0_arm64
WHEEL_BINARY ?= eval-hub-darwin-arm64

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
	npm i @redocly/cli@latest

clean-docs:
	rm -f docs/openapi.yaml docs/openapi.json docs/openapi-internal.yaml docs/openapi-internal.json docs/*.html

generate-public-docs: ${REDOCLY_CLI}
	npm update @redocly/cli
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
