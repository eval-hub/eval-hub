#!/bin/bash

# Script to stop the MLflow server locally

echo "🛑 Stopping MLflow server..."
pkill -f "mlflow.server" || echo "No MLflow server process found"
# Wait for the server to stop (loop for up to 5 seconds)
timeout=50  # 50 iterations * 0.1 seconds = 5 seconds
iterations=0
while [ $iterations -lt $timeout ]; do
    if ! pgrep -f "mlflow.server" > /dev/null; then
        echo "🛑 MLflow server stopped"
        exit 0
    fi
    sleep 0.1
    iterations=$((iterations + 1))
done
# If we get here, the server is still running after timeout
echo "❌ MLflow server is still running after 5 seconds"
exit 1
