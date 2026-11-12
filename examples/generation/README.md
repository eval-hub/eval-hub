# Collection-generation API demo

This directory demonstrates the collection-generation endpoints using the
temporary, stateless fixture implementation. The permanent request and response
contract is defined in `docs/src/openapi.yaml`; the `mock=true` query parameter
described below is only a local integration-test switch and is intentionally not
part of that OpenAPI contract.

## Prerequisites

- A running kind cluster deployed with `make dev-deploy`.
- The EvalHub image built from the branch containing the generation API fixture.
- An operator deployment that routes and authorizes the generation paths to the
  EvalHub service. Until the operator mapping is deployed, authenticated calls
  may return `403` even if the service image has the handlers.
- `kubectl`, `kind`, `curl`, and `jq` on `PATH`.

## Run the scripted demo

From the EvalHub repository root:

```bash
make dev-generation-demo
```

The script uses the `evalhub-dev` cluster, its private kubeconfig, and the
localhost NodePort at `https://127.0.0.1:18443` by default. It uses the recorded
Docker or Podman runtime, checks EvalHub readiness, and mints a short-lived token
for `system/evalhub-local-service`. To select a different cluster or pass an
already-authorized token:

```bash
make dev-generation-demo KIND_CLUSTER_NAME=my-kind EVALHUB_HOST_PORT=18443
```

Or export `API_TOKEN` before running the target. The script never prints the
token, writes its curl config into a mode-0600 temporary directory, and removes
that directory on exit. It does not build images, create a collection, or submit
an evaluation job.

The demo verifies these fixture-only behaviors:

1. Omitting `mock=true` returns HTTP 503 with
   `message_code: collection_generation_not_configured`.
2. `GET .../generation/models?mock=true` returns the single
   `mock-generator` profile.
3. `POST .../generation?mock=true` returns HTTP 202, normalized request defaults,
   pending status, and a relative `Location` ending in `?mock=true`.
4. Polling `Location` returns the static completed proposal. The JSON collection,
   YAML serialization, selected benchmark explanations, and action summaries
   are checked by the Go tests.
5. `DELETE` is a repeatable no-op for the fixed fixture ID; unknown IDs return
   404.

The mock switch must be exactly the single query value `true`. Other values or
repeated `mock` parameters return HTTP 400. This fixture is not a real generation
implementation: it stores no request, has no running phase or progress, and
returns the same completed proposal on every poll.

## Send the requests manually

`demo.http` contains equivalent REST Client requests. Set the environment
variables consumed by the file; do not paste a token into the file:

```bash
export EVALHUB_API_BASE_URL=https://127.0.0.1:18443
export API_TOKEN="$(kubectl --kubeconfig "$HOME/.config/evalhub/kubeconfigs/evalhub-dev.yaml" \
  -n system create token evalhub-local-service --duration=1h)"
```

Open `demo.http` in a REST Client-compatible editor and run the requests in
order. The POST response's `Location` is the polling URL. For cleanup, remove
the token from the shell:

```bash
unset API_TOKEN
```
