# Release notes template

Use this shape for the GitHub release body. Drop sections with nothing useful to say.
Do not leave `TBD` placeholders in a published release.

```markdown
## Summary

<!-- 2–4 sentences: what shipped and why a user might upgrade -->

## Upgrade impact

<!-- Required. Be explicit. Examples:
- No breaking changes expected for API clients on /api/v1.
- Breaking: <what changed>, <who is affected>, <what to do>.
- Config/env: <new or removed keys>.
- Images: pin to <version> tag; see COMPATIBILITY.md.
- Align eval-hub-sdk only if this release documents a required client bump.
-->

## Breaking changes

<!-- Omit entire section if none. List each break with mitigation. -->

## Highlights

<!-- Major user-facing features / changes. Link PRs: (#123) -->

## Bug fixes

<!-- User-visible fixes only -->

## Dependencies and security

<!-- Notable dependency bumps, CVE fixes, Go/toolchain bumps -->

## Documentation

<!-- Significant docs/OpenAPI changes worth calling out -->

## Contributors

<!-- Optional: thank non-bot contributors for this range -->
```

## Tone

- Write for operators and API consumers, not for commit archaeology.
- Prefer concrete upgrade steps over vague “various improvements”.
- When unsure whether something breaks clients, check `COMPATIBILITY.md` and
  OpenAPI diffs in the range; if still unsure, flag it under Upgrade impact as
  “verify before upgrading” rather than omitting it.
