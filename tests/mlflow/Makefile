.PHONY: install-mlflow run-mlflow stop-mlflow cls test-godog-server

## install-mlflow: Download and install MLflow server
install-mlflow:
	@echo "📥 Installing MLflow server..."
	@./scripts/download_mlflow.sh

## run-mlflow: Run MLflow server locally in the background
run-mlflow:
	@echo "🛑 Stopping MLflow server..."
	-@make stop-mlflow
	-@rm -f mlflow.db tests/features/test_mlflow_${MLFLOW_PORT}.db
	@echo "🚀 Starting MLflow server..."
	@echo "   Host: $(MLFLOW_HOST)"
	@echo "   Port: $(MLFLOW_PORT)"
	@echo "   Backend: $(MLFLOW_BACKEND_STORE_URI)"
	@echo "   Artifacts: $(MLFLOW_DEFAULT_ARTIFACT_ROOT)"
	@echo ""
	@MLFLOW_HOST=$(MLFLOW_HOST) \
	 MLFLOW_PORT=$(MLFLOW_PORT) \
	 MLFLOW_BACKEND_STORE_URI=$(MLFLOW_BACKEND_STORE_URI) \
	 MLFLOW_DEFAULT_ARTIFACT_ROOT=$(MLFLOW_DEFAULT_ARTIFACT_ROOT) \
	 ./scripts/run_mlflow.sh
	@echo "   MLflow server started in background. Use 'make stop-mlflow' to stop it."
	@echo "" #@echo "   You can use 'make test-godog-server' to run BDD tests with the server automatically."
	@echo ""
	@echo "   To use the server in your tests:"
	@echo "   - Set MLFLOW_TRACKING_URI=http://127.0.0.1:5000 in your test environment"
	@echo "" # @echo "   - Run tests with 'make test-godog'"
	@echo ""
	@echo "   To stop the server:"
	@echo "   make stop-mlflow"

## stop-mlflow: Stop MLflow server (if running)
stop-mlflow:
	./scripts/stop_mlflow.sh

MLFLOW_TRACKING_URI ?= http://127.0.0.1:5000

## test-godog-server: Run godog tests with automatic server management
test-godog-server: run-mlflow ; $(info The MLflow server was successfully started)
	@echo "🧪 Running godog BDD tests with test server..."
	@MLFLOW_TRACKING_URI=${MLFLOW_TRACKING_URI} go test -v -tags=godog ./...
	@make stop-mlflow

.PHONY: cls
cls:
	printf "\33c\e[3J"
