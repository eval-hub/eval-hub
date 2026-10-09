# Inspect HLE

The HLE scorer and dataset settings are defined in
[knowledge-reasoning-v1.yaml](../../config/collections/knowledge-reasoning-v1.yaml).
Collection jobs inherit those settings; supply `model_roles.grader` through the
benchmark's request overrides. Provider benchmark entries do not supply parameter
defaults for standalone jobs.

## Runtime compatibility

[inspect.yaml](../../config/providers/inspect.yaml) pins the Inspect image by tag
and digest. Its HLE configuration uses `inspect-ai` 0.3.276, `inspect-evals` 0.23.0,
and HLE task revision `7-E`. The versioned
[task metadata](https://github.com/UKGovernmentBEIS/inspect_evals/blob/v0.23.0/src/inspect_evals/hle/eval.yaml)
and [changelog](https://github.com/UKGovernmentBEIS/inspect_evals/blob/v0.23.0/src/inspect_evals/hle/README.md#changelog)
describe that revision. Older task signatures and metric keys require separately
validated adapter compatibility handling.

Operator-managed deployments must propagate the image pin into their provider
configuration. The pin applies to the entire Inspect provider.

## Standalone request

[request.json](request.json) is a template for the target model, grader binding,
and sample limit. [prepare_request.py](prepare_request.py) adds the collection's
HLE parameters and primary metric, with the template's parameters taking
precedence. This keeps the scorer configuration in one place.

1. Edit the template's target model URL/name, Secret reference, and grader model.
2. Prepare the referenced Secret in the request's tenant namespace. The example
   uses `hf-token` for dataset access, `judge_url` and `judge_api-key` for the grader,
   plus any target authentication and CA material required by your deployment.
   The grader uses the sidecar URL and the `judge_api-key:ref` credential reference.
3. With Python and PyYAML installed, generate the complete request from the
   repository root:

   ```bash
   python examples/inspect-hle/prepare_request.py > /tmp/hle-request.json
   ```

4. Set the deployment variables below and submit the generated request:

   ```bash
   curl --fail-with-body --cacert "$EVALHUB_CA_CERT" \
     -H "Authorization: Bearer $EVALHUB_TOKEN" \
     -H "X-Tenant: $EVALHUB_TENANT" \
     -H 'Content-Type: application/json' \
     --data-binary @/tmp/hle-request.json \
     "$EVALHUB_BASE_URL/api/v1/evaluations/jobs"
   ```

The example overrides the collection's screening cap with a three-example limit.
Adjust `num_examples` in the template for your evaluation size.
