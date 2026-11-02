# Kubernetes Functional Verification Tests (FVT)

## Overview

These tests validate Kubernetes resources created by eval-hub in a real cluster. They focus on Job and ConfigMap creation, metadata/spec correctness, and deletion behavior.

## Run

### Prerequisites

- Go 1.26+
- A running eval-hub service that is configured with Kubernetes runtime
- Access to the Kubernetes cluster where resources are created (read-only is sufficient)

Required env vars:
- `SERVER_URL` (API base URL)
- `KUBERNETES_NAMESPACE`
- `AUTH_TOKEN`
- `KUBECONFIG` (required for cluster access)

If any required env var is missing, the tests are skipped.

Optional:
- `FVT_CA_CERT_PATH` for a PEM bundle containing the private CA for the EvalHub API
- `K8S_TEST_DEBUG=true` for verbose test logs

For a private CA, set `FVT_CA_CERT_PATH=/absolute/path/to/cluster-ca.pem`.
The bundle is added to system trust while certificate and hostname validation
remain enabled. An unreadable or invalid bundle fails test setup. Without this
setting, system trust is used. `SKIP_TLS_VERIFY` is no longer supported.
The Kubernetes API client separately uses CA settings from kubeconfig or
in-cluster configuration; this setting applies to EvalHub HTTP requests only.

### Execute

From the repo root:
```bash
go test -v ./tests/kubernetes/features
```

## Scenarios Covered

- **Resource creation**: Jobs/ConfigMaps created per benchmark with expected labels and spec fields.
- **Deletion**: soft delete keeps resources, hard delete removes them (polled until gone).
