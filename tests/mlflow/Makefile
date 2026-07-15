.PHONY: help clean init-python install-mlflow start-mlflow stop-mlflow cls test-godog-server test-unit-server

# Local Python environment (uv). Isolated from the repo-root .venv used for FVT.
VENV_DIR ?= .venv
PYTHON ?= 3.14

MLFLOW_OLD_VERSION ?= 3.8.1
MLFLOW_LATEST_VERSION ?= 3.14.0
MLFLOW_VERSION ?= $(MLFLOW_LATEST_VERSION)

MLFLOW_HOST ?= 127.0.0.1
MLFLOW_PORT ?= 5000
MLFLOW_BACKEND_STORE_URI ?= sqlite:///bin/mlflow.db
MLFLOW_DEFAULT_ARTIFACT_ROOT ?= ./bin/mlruns
MLFLOW_TRACKING_URI ?= http://127.0.0.1:5000
MLFLOW_ENABLE_WORKSPACES ?= false
MLFLOW_DISABLE_SECURITY_MIDDLEWARE ?= false
MLFLOW_LOG_FILE ?= bin/mlflow_$(MLFLOW_PORT).log

help: ## Show MLflow dev targets
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

clean: ## Remove bin/ artifacts (DB, logs, mlruns)
	@echo "🧹 Cleaning up MLflow server..."
	@rm -fr bin

init-python: ## Create a local Python virtualenv with uv (required before install-mlflow)
	@if ! command -v uv >/dev/null 2>&1; then \
		echo "❌ uv is not installed. Install from https://docs.astral.sh/uv/"; \
		exit 1; \
	fi
	@if [ ! -d "$(VENV_DIR)" ]; then \
		echo "Creating uv virtual environment at $(VENV_DIR) (Python $(PYTHON))..."; \
		uv venv $(VENV_DIR) --python $(PYTHON); \
	else \
		echo "Virtual environment already exists at $(VENV_DIR)"; \
	fi

install-mlflow: init-python ## Install MLflow into the uv virtualenv
	@echo "📥 Installing MLflow server (version $(MLFLOW_VERSION))..."
	@MLFLOW_VERSION=$(MLFLOW_VERSION) VENV_DIR=$(VENV_DIR) ./scripts/download_mlflow.sh

start-mlflow: install-mlflow ## Run MLflow server locally in the background
	@echo "🛑 Stopping MLflow server..."
	-@make stop-mlflow
	-@rm -f bin/mlflow.db tests/features/test_mlflow_${MLFLOW_PORT}.db
	@echo "🚀 Starting MLflow server..."
	@echo "   Host: $(MLFLOW_HOST)"
	@echo "   Port: $(MLFLOW_PORT)"
	@echo "   Backend: $(MLFLOW_BACKEND_STORE_URI)"
	@echo "   Artifacts: $(MLFLOW_DEFAULT_ARTIFACT_ROOT)"
	@echo "   Log file: $(MLFLOW_LOG_FILE)"
	@echo ""
	@MLFLOW_HOST=$(MLFLOW_HOST) \
	 MLFLOW_PORT=$(MLFLOW_PORT) \
	 MLFLOW_BACKEND_STORE_URI=$(MLFLOW_BACKEND_STORE_URI) \
	 MLFLOW_DEFAULT_ARTIFACT_ROOT=$(MLFLOW_DEFAULT_ARTIFACT_ROOT) \
	 MLFLOW_ENABLE_WORKSPACES=$(MLFLOW_ENABLE_WORKSPACES) \
	 MLFLOW_LOG_FILE=$(MLFLOW_LOG_FILE) \
	 VENV_DIR=$(VENV_DIR) \
	 ./scripts/run_mlflow.sh
	@echo "   MLflow server started in background. Use 'make stop-mlflow' to stop it."
	@echo ""
	@echo "   To use the server in your tests:"
	@echo "   export MLFLOW_TRACKING_URI=$(MLFLOW_TRACKING_URI)"

start-mlflow-with-logs: start-mlflow ## Run MLflow server locally in the background and tail the log file
	`@echo` "Tailing the log file:"
	`@echo` ""
	`@tail` -f $(MLFLOW_LOG_FILE)

stop-mlflow: ## Stop MLflow server (if running)
	./scripts/stop_mlflow.sh

test-godog-server: start-mlflow ; $(info The MLflow server was successfully started) ## Run godog tests with automatic server management
	@echo "🧪 Running godog BDD tests with test server..."
	@MLFLOW_TRACKING_URI=$(MLFLOW_TRACKING_URI) go test -v -tags=godog ./...
	@make stop-mlflow

.PHONY: cls
cls:
	printf "\33c\e[3J"

test-unit-server: ## Run MLflow integration unit tests (auto-starts server per case, skips if unavailable)
	@echo "🧪 Running MLflow integration unit tests..."
	@go test -v ../../internal/eval_hub/mlflow -run TestMLFlowIntegration -count=1
