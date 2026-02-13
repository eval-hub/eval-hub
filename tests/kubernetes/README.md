# Kubernetes Functional Verification Tests (FVT)

## Overview

These tests validate Kubernetes resources created by eval-hub in a real cluster. They focus on Job and ConfigMap creation, metadata/spec correctness, and deletion behavior.

## Run

### Prerequisites

- Go 1.21+
- A running eval-hub service that is configured with Kubernetes runtime
- Access to the Kubernetes cluster where resources are created (read-only is sufficient)

Required env vars:
- `SERVICE_URL` (API base URL)
- `KUBERNETES_NAMESPACE`
- `AUTH_TOKEN`

Optional:
- `SKIP_TLS_VERIFY=true` for self-signed TLS
- `K8S_TEST_DEBUG=true` for verbose test logs

### Execute

From the repo root:
```bash
go test -v ./tests/kubernetes/features
```

## Scenarios Covered

- **Resource creation**: Jobs/ConfigMaps created per benchmark with expected labels and spec fields.
- **Deletion**: soft delete keeps resources, hard delete removes them (polled until gone).
