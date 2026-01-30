# API Documentation

This directory contains the OpenAPI specifications and related assets for the Eval Hub API.

## Swagger UI

- Public documentation: [index.html](https://raw.githubusercontent.com/julpayne/eval-hub/refs/heads/api-updates/docs/index.html)
- Internal documentation: [index-internal.html](index-internal.html)

## Files

| File | Description |
|------|-------------|
| **src/openapi.yaml** | Single source of truth for the API. OpenAPI 3.1.0 spec with all paths, schemas, and (optionally) `x-internal`-marked content. Edit this file when changing the API contract. |
| **redocly.yaml** | Redocly CLI config. Defines two API entry points (`internal@latest` and `external@latest`), both rooted at `src/openapi.yaml`. The external bundle uses the `remove-x-internal` decorator to strip internal-only content. This file might also include some rules to avoid warnings when using the redocly vscode plugin. |
| **openapi.yaml/.json** | **Generated.** Public API bundle produced by `make generate-public-docs`. Built from `src/openapi.yaml` with internal-only paths/schemas removed. Served at `/openapi.yaml` and used by Swagger UI at `/docs`. |
| **openapi-internal.yaml/.json** | **Generated.** Internal API bundle produced by `make generate-public-docs`. Full spec from `src/openapi.yaml` including `x-internal` content. For internal tooling and docs. |
| **preview.html** | **Generated.** Static redoc UI page with the OpenAPI spec **inlined**. This generated page avoids CORS issues when loading a local file. |
| **preview-internal.html** | **Generated.** Static redoc UI page with the OpenAPI spec **inlined**. This generated page avoids CORS issues when loading a local file. |
| **old/openapi.yaml** / **old/openapi.json** | Legacy snapshots of the spec; kept for reference. Do not use for code generation or serving. |

## Generating the public (and internal) docs

From the **repository root**:

```bash
make generate-public-docs
```

This target:

1. Ensures the Redocly CLI is available (installs `@redocly/cli` via npm if needed).
2. Runs **external** bundle to **openapi.yaml** and **openapi.json** (with `x-internal` content removed).
3. Runs **internal** bundle to **openapi-internal.yaml** (full spec).
4. Runs **build-standalone-html.js** to produce **index-standalone.html** (spec inlined for local file viewing).

Run `make generate-public-docs` after editing **src/openapi.yaml** so that **openapi.yaml**, **openapi-internal.yaml**, and **index-standalone.html** stay in sync. The server serves **openapi.yaml** at `/openapi.yaml` and **index.html** at `/docs` (Swagger UI).

## Viewing docs locally (avoiding CORS)

If you open **index.html** directly in the browser (`file:///path/to/docs/index.html`), the page tries to fetch `openapi.yaml` via a relative URL. Browsers treat that as a cross-origin request from the `file://` origin and block it (CORS / same-origin policy), so Swagger UI shows no spec.

**Options:**

1. **Use the standalone file** – Open **index-standalone.html** instead. It has the spec inlined, so no fetch and no CORS. Build it with `make generate-public-docs` (it is generated automatically).
2. **Serve over HTTP** – Run a local server from this directory (e.g. `python3 -m http.server 8080` in `docs/`) and open `http://localhost:8080/` or `http://localhost:8080/index.html`. Then `openapi.yaml` is same-origin and loads correctly.
3. **Use the running app** – If the Eval Hub server is running, open `http://127.0.0.1:8080/docs`; the server serves the same Swagger UI and spec.

## Related Make targets

- **verify-api-docs** – Lint `docs/openapi.yaml` with Redocly.
- **generate-ignore-file** – Generate a Redocly ignore file from current lint output (e.g. `.redocly.lint-ignore.yaml`).

These targets are defined in the top-level **Makefile**.
