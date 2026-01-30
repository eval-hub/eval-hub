.PHONY: help autoupdate-precommit pre-commit clean build start-service stop-service lint test test-fvt-server test-all test-coverage test-fvt-coverage test-all-coverage install-deps update-deps get-deps fmt vet update-deps

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

BUILD_PACKAGE ?= main
FULL_BUILD_NUMBER ?= 0.0.1
LDFLAGS_X = -X "${BUILD_PACKAGE}.Build=${FULL_BUILD_NUMBER}" -X "${BUILD_PACKAGE}.BuildDate=$(DATE)"
LDFLAGS = -buildmode=exe ${LDFLAGS_X}

build: ## Build the binary
	@echo "Building $(BINARY_NAME) with ${LDFLAGS}"
	@mkdir -p $(BIN_DIR)
	@go build -ldflags "${LDFLAGS}" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

SERVER_PID_FILE ?= $(BIN_DIR)/pid

${SERVER_PID_FILE}:
	rm -f "${SERVER_PID_FILE}" && true

SERVICE_LOG ?= $(BIN_DIR)/service.log

start-service: ${SERVER_PID_FILE} build ## Run the application in background
	@echo "Running $(BINARY_NAME) on port $(PORT)..."
	@./scripts/start_server.sh "${SERVER_PID_FILE}" "${BIN_DIR}/$(BINARY_NAME)" "${SERVICE_LOG}" ${PORT} ""

stop-service:
	-./scripts/stop_server.sh "${SERVER_PID_FILE}"

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

test-fvt: ## Run FVT (Functional Verification Tests) using godog
	@echo "Running FVT tests..."
	@go test -v ./tests/features/...

test-all: test test-fvt ## Run all tests (unit + FVT)

SERVER_URL ?= http://localhost:8080

test-fvt-server: start-service ## Run FVT tests using godog against a running server
	`@SERVER_URL`="${SERVER_URL}" make test-fvt; status=$$?; make stop-service; exit $$status

test-coverage: ## Run unit tests with coverage
	@echo "Running unit tests with coverage..."
	@mkdir -p $(BIN_DIR)
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage.out -covermode=atomic ./internal/... ./cmd/...
	@go tool cover -html=$(BIN_DIR)/coverage.out -o $(BIN_DIR)/coverage.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage.html"

test-fvt-coverage: ## Run integration (FVT) tests with coverage
	@echo "Running integration (FVT) tests with coverage..."
	@mkdir -p $(BIN_DIR)
	@go test -v -race -coverprofile=$(BIN_DIR)/coverage-fvt.out -covermode=atomic ./tests/features/...
	@go tool cover -html=$(BIN_DIR)/coverage-fvt.out -o $(BIN_DIR)/coverage-fvt.html
	@echo "Coverage report generated: $(BIN_DIR)/coverage-fvt.html"

test-all-coverage: test-coverage test-fvt-coverage ## Run all tests (unit + FVT) with coverage

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
	cd docs && ${REDOCLY_CLI} bundle external@latest --output openapi.yaml --remove-unused-components
	cd docs && ${REDOCLY_CLI} bundle external@latest --ext json --output openapi.json
	cd docs && ${REDOCLY_CLI} bundle internal@latest --output openapi-internal.yaml --remove-unused-components
	cd docs && ${REDOCLY_CLI} bundle internal@latest --ext json --output openapi-internal.json
	cd docs && ${REDOCLY_CLI} build-docs openapi.json --output=index-public.html
	cd docs && ${REDOCLY_CLI} build-docs openapi-internal.json --output=index.html

verify-api-docs: ${REDOCLY_CLI}
	${REDOCLY_CLI} lint ./docs/openapi.yaml
	echo "See https://editor.swagger.io/?url=https://raw.githubusercontent.com/julpayne/eval-hub/refs/heads/api-updates/docs/openapi.yaml"

generate-ignore-file: ${REDOCLY_CLI}
	${REDOCLY_CLI} lint --generate-ignore-file ./docs/openapi.yaml
