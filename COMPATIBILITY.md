# EvalHub Compatibility

This document describes compatibility for this repository: the EvalHub server, its REST
API contract, published container images, and how clients (such as the Python SDK) relate
to a given server release.

> **Note:** All components are currently pre-1.0. Breaking changes may occur between
> minor releases. Prefer pinned image tags and release notes over `latest`.

## Sources of truth

| Concern | Where to look |
|---------|----------------|
| Server version / release notes | [GitHub releases](https://github.com/eval-hub/eval-hub/releases) and the `VERSION` file |
| REST API contract | OpenAPI under [`docs/openapi.yaml`](docs/openapi.yaml) (source: [`docs/src/`](docs/src/)); live docs at [eval-hub.github.io/eval-hub](https://eval-hub.github.io/eval-hub/) |
| Python SDK versions | [eval-hub-sdk](https://github.com/eval-hub/eval-hub-sdk) releases / PyPI |
| Image tags published by CI | [quay.io/evalhub/evalhub](https://quay.io/repository/evalhub/evalhub?tab=tags) (see [Container Image Tag Policy](#container-image-tag-policy)) |

## Versioning policy

- **EvalHub** (server) follows [SemVer](https://semver.org/). While the major version is
  `0`, minor bumps may include breaking API changes; treat them as potentially incompatible
  unless release notes say otherwise. After `1.0.0`, major bumps indicate breaking API changes.
- **SDK** uses [PEP 440](https://peps.python.org/pep-0440/) versioning (including
  pre-releases such as `0.1.0a8`). Alpha / pre-release SDK versions may break between
  builds; prefer a stable SDK release when talking to a pinned server.

### Client guidance (pre-1.0)

- Prefer an SDK release that targets the same EvalHub **minor** line as the server you call
  (for example SDK `0.4.x` with server `0.4.x`).
- Older clients may continue to work against newer servers when only additive API changes
  landed, but that is not a guarantee before `1.0.0`.
- When in doubt, compare the server OpenAPI for your deployed version with the SDK’s
  expected API surface, or upgrade the client with the server.

## API compatibility

The compatibility boundary for HTTP clients is the versioned REST API under `/api/v1`.
Behavioural contracts live in the OpenAPI specification for that release.

### What counts as a breaking change

- Removing or renaming an API endpoint
- Removing or renaming a request/response field
- Changing the type or semantics of an existing field
- Changing authentication or authorisation requirements
- Removing support for a previously accepted query parameter

### What does not count as a breaking change

- Adding new endpoints
- Adding new optional fields to requests or responses
- Adding new query parameters with defaults that preserve existing behaviour
- Bug fixes that align behaviour with documented API contracts

Release notes (or the PR description for a version bump) should call out intentional
breaking changes when they occur.

## Container Image Tag Policy

The EvalHub container image is published to `quay.io/evalhub/evalhub`. CI produces the
following tags on pushes to observed branches (`main`, `develop`) and on version tags
(`v*`):

| Tag | When applied | Example |
|-----|-------------|---------|
| `latest` | Every push to an observed branch or a version tag | Always points to the most recent image pushed |
| `<branch>` | Branch push | `main`, `develop` |
| `<branch>-<sha>` | Branch push | `main-a1b2c3d…` |
| `<version>` | Tag push (`v1.2.3`) | `1.2.3` |
| `<major>.<minor>` | Tag push (`v1.2.3`) | `1.2` |

Non-release branch images may carry a Quay expiry annotation; semver tags from `v*`
releases are intended for durable pins.

### Production deployments

Production environments should pin a specific semver tag (for example `0.4.4`) rather
than `latest`. The `latest` tag is useful for development and testing but may point to an
unreleased commit on `main` or `develop`.

## Keeping this document useful

Update this file when:

- The API compatibility rules change
- Image tagging behaviour in CI changes
- The preferred discovery links (releases, Quay, SDK) change

Do **not** add a multi-column version matrix here. Record server releases in GitHub
Releases; keep client version history in the SDK repository.

## Remark: ODH and RHOAI

Which EvalHub image ships in a given Open Data Hub or Red Hat OpenShift AI release is
decided outside this repository (typically via `evalHubImage` in TrustyAI Service Operator
overlays such as `params.env`). Consult
[trustyai-service-operator](https://github.com/trustyai-explainability/trustyai-service-operator)
for product pins; this document does not track those combinations.
