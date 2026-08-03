# API Documentation

This directory contains the OpenAPI specifications and related assets for the Eval Hub API.

## Swagger UI

- Public documentation:
  - [Redocly website](https://eval-hub.github.io/eval-hub/)
  - [Swagger editor](https://editor.swagger.io/?url=https://raw.githubusercontent.com/eval-hub/eval-hub/refs/heads/main/docs/openapi.yaml) - **Note:** When testing locally with the Swagger editor, ensure the server is started with `--local` to enable CORS headers middleware ([cors_middleware.go](../internal/eval_hub/server/cors_middleware.go))
- Internal documentation: [index-private.html](index-private.html)

## Files

These are the files in the `docs` directory.

| File | Description |
|------|-------------|
| **src/openapi.yaml** | Single source of truth for the API. OpenAPI 3.1.0 spec with all paths, schemas, and (optionally) `x-internal`-marked content. Edit this file when changing the API contract. |
| **redocly.yaml** | Redocly CLI config. Defines two API entry points (`internal@latest` and `external@latest`), both rooted at `src/openapi.yaml`. The external bundle uses the `remove-x-internal` decorator to strip internal-only content. This file might also include some rules to avoid warnings when using the redocly vscode plugin. |
| **openapi.yaml/.json** | **Generated.** Public API bundle produced by `make generate-public-docs`. Built from `src/openapi.yaml` with internal-only paths/schemas removed. Served at `/openapi.yaml` and used by Swagger UI at `/docs`. |
| **openapi-internal.yaml/.json** | **Generated.** Internal API bundle produced by `make generate-public-docs`. Full spec from `src/openapi.yaml` including `x-internal` content. For internal tooling and docs. |
| **index-public.html** | **Generated.** Public Redoc docs page produced by `make generate-public-docs`. Copied to `index.html`. |
| **index-private.html** | **Generated.** Internal Redoc docs page with `x-internal` content included. |

## YAML `description:` block scalars

In OpenAPI YAML under `docs/src/`, multi-line `description:` values use YAML [block scalars](https://yaml.org/spec/1.2.2/#81-block-scalar-styles). The character(s) immediately after `description:` control how line breaks and the final newline are handled:

| Indicator | Style | Meaning |
|-----------|--------|---------|
| *(none)* | Plain / flow | Single-line string on the same line as `description:`. Fine for short text with no intentional line breaks. |
| `\|` | Literal | Newlines in the indented block are **kept**. Use for multi-paragraph docs, lists, or any text where line breaks should appear in the rendered description. |
| `>` | Folded | Single newlines in the indented block are **folded into spaces** (paragraphs separated by a blank line stay as separate paragraphs). Use for long prose that should read as continuous sentences. |
| `\|-` / `>-` | Literal / folded + **strip** | Same as `\|` or `>`, but the **final** newline of the block is removed. Prefer when a trailing `\n` would be undesirable (for example short field descriptions). |
| `\|+` / `>+` | Literal / folded + **keep** | Same as `\|` or `>`, but **all** trailing newlines at the end of the block are kept. Rarely needed for API descriptions. |

Examples:

```yaml
# Literal: blank lines and newlines are preserved
description: |
  First paragraph.

  Second paragraph with a real line break above.

# Folded: adjacent lines become one paragraph; blank line starts a new one
description: >
  This line and the next are joined with a space
  into a single paragraph.

  This is a second paragraph.

# Folded + strip: fold lines and drop the final newline
description: >-
  Short multi-line source that renders as one line without a trailing newline.
```

This repository commonly uses `|` for structured multi-paragraph schema docs and `>` / `>-` for flowing prose on paths and fields.

## Generating the public (and internal) docs

From the **repository root**:

```bash
make generate-public-docs
```

This target:

1. Ensures the Redocly CLI is available (installs `@redocly/cli` via npm if needed).
2. Runs **external** bundle to **openapi.yaml** and **openapi.json** (with `x-internal` content removed).
3. Runs **internal** bundle to **openapi-internal.yaml** (full spec).
4. Runs `redocly build-docs` to produce **index-public.html** and **index-private.html**, then copies **index-public.html** to **index.html**.

Run `make generate-public-docs` after editing **src/openapi.yaml** so that **openapi.yaml**, **openapi-internal.yaml**, **index-public.html**, and **index-private.html** stay in sync. The server serves **openapi.yaml** at `/openapi.yaml` and **index.html** at `/docs` (Swagger UI).

## Viewing docs locally (avoiding CORS)

If you open **index.html** directly in the browser (`file:///path/to/docs/index.html`), the page tries to fetch `openapi.yaml` via a relative URL. Browsers treat that as a cross-origin request from the `file://` origin and block it (CORS / same-origin policy), so Swagger UI shows no spec.

**Options:**

1. **Use the generated Redoc page** – Open **index-public.html** (or **index-private.html** for internal docs). These have the spec inlined, so no fetch and no CORS. Build them with `make generate-public-docs`.
2. **Serve over HTTP** – Run a local server from this directory (e.g. `python3 -m http.server 8080` in `docs/`) and open `http://localhost:8080/` or `http://localhost:8080/index.html`. Then `openapi.yaml` is same-origin and loads correctly.
3. **Use the running app** – If the Eval Hub server is running, open `http://127.0.0.1:8080/docs`; the server serves the same Swagger UI and spec.

## Related Make targets

- **verify-api-docs** – Lint `docs/openapi.yaml` with Redocly.
- **generate-ignore-file** – Generate a Redocly ignore file from current lint output (e.g. `.redocly.lint-ignore.yaml`).

These targets are defined in the top-level **Makefile**.
