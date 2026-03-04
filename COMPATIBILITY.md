# EvalHub Compatibility Matrix

This document tracks version compatibility between EvalHub components and the RHOAI platform.

## Version Matrix

| EvalHub | SDK | RHOAI | Backwards Compatible | Notes |
|---------|-----|-------|----------------------|-------|
| 0.1.0 | 0.1.0a8 | 3.4-ea1 | N/A | Initial release |

> **Note:** All components are currently pre-1.0 and breaking changes may occur between releases. The version combinations listed above have been verified to work together.

## Versioning Policy

- **EvalHub** (server) follows [SemVer](https://semver.org/). Major version bumps indicate breaking API changes.
- **SDK** uses [PEP 440](https://peps.python.org/pep-0440/) pre-release versioning (e.g. `0.1.0a8`). Alpha releases (`aN`) may contain breaking changes between versions.
- **RHOAI** versions follow Red Hat's release naming convention.

## Backwards Compatibility

The **Backwards Compatible** column indicates whether the EvalHub REST API is backwards compatible with the immediately preceding version:

- **Yes** -- No breaking changes. Existing clients continue to work without modification.
- **No** -- Breaking changes introduced. Clients may need updates (see notes for details).
- **N/A** -- First release or no previous version to compare against.

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
