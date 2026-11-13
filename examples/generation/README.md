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
./examples/generation/demo.sh
```

The script uses the `evalhub-dev` cluster, its private kubeconfig, and the
localhost NodePort at `https://127.0.0.1:18443` by default. It uses the recorded
Docker or Podman runtime, checks EvalHub readiness, and mints a short-lived token
for `system/evalhub-local-service`. To select a different cluster or pass an
already-authorized token:

```bash
KIND_CLUSTER_NAME=my-kind EVALHUB_HOST_PORT=18443 ./examples/generation/demo.sh
```

Or export `API_TOKEN` before running the script. The script never prints the
token, writes its curl config into a mode-0600 temporary directory, and removes
that directory on exit. It does not build images, create a collection, or submit
an evaluation job.

The demo verifies these fixture-only behaviors:

1. Omitting the mock parameter returns HTTP 503 with
   `message_code: collection_generation_not_configured`.
2. `POST .../generation?mock=true` accepts a caller-supplied generator model
   connection and returns HTTP 202 with normalized request defaults,
   pending status, and a relative `Location` ending in `?mock=complete`.
3. `GET ...?mock=progress` returns a fixed running response with the API's
   `status.progress` object and last completed action.
4. `GET ...?mock=complete` (the value used by `Location`) returns a completed
   response with two selected benchmarks, two rejected benchmarks, an
   explanation for each, and several action summaries. Completed status omits
   `status.progress`, as required by the API contract. The JSON collection,
   YAML serialization, explanations, and both status shapes are checked by tests.
5. `DELETE` is a repeatable no-op for the fixed fixture ID; unknown IDs return
   404.

`POST` and `DELETE` require exactly one `mock=true` query value. `GET` accepts
exactly one of `mock=progress`, `mock=complete`, or the compatibility alias
`mock=true` (which returns the completed fixture). Other values or repeated
`mock` parameters return HTTP 400. These responses are static examples, not a
real generation lifecycle: polling does not advance the resource from running
to completed, and the fixture stores no request or state.
The fixture accepts a model URL and name but never calls that endpoint or reads
its optional Secret reference. Its fixed GET response contains an example model
and does not retain the model submitted in POST.

## Send the requests manually

`demo.http` contains equivalent REST Client requests. Replace the two marked
placeholders in that file with the values from these commands:

```bash
printf '%s\n' 'https://127.0.0.1:18443'
KIND_CLUSTER_NAME=evalhub-dev
KCFG="${XDG_CONFIG_HOME:-$HOME/.config}/evalhub/kubeconfigs/${KIND_CLUSTER_NAME}.yaml"
kubectl --kubeconfig "$KCFG" -n system create token evalhub-local-service --duration=1h
```

Use the first command's output for `@baseUrl` and the last command's output for
`@token`. For a custom dev-deploy host port or cluster name, substitute the
values used for that deployment. Do not commit the populated token; restore the
placeholder after the demo. Run the POST, progress GET, complete GET, and DELETE
requests in order.
