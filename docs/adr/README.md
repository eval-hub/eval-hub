# Architecture Decision Records

This directory holds the Architecture Decision Records (ADRs) for EvalHub — short
documents capturing a significant architectural decision, its context, and its
consequences.

## Index

| ADR | Title | Status | Date |
| :---- | :---- | :---- | :---- |
| [0001](0001-sandboxed-evaluation-runtime-for-agentic-frameworks.md) | Sandboxed evaluation runtime for agentic frameworks | Proposed | 2026-09-30 |

## Conventions

- **Format:** [MADR](https://adr.github.io/madr/) / Nygard-style — a numbered file per
  decision with `Status`, `Context`, `Decision`, `Alternatives considered`, and
  `Consequences` sections.
- **Filename:** `NNNN-kebab-case-title.md`, where `NNNN` is the next zero-padded number
  in sequence.
- **Status:** one of `Proposed`, `Accepted`, `Deprecated`, or `Superseded by ADR-NNNN`.
  ADRs are immutable once accepted — to change a decision, add a new ADR that supersedes
  the old one (link both ways) rather than editing the original.
- **Index:** add a row to the table above whenever you add an ADR.

## Adding a new ADR

1. Copy the section layout of [ADR 0001](0001-sandboxed-evaluation-runtime-for-agentic-frameworks.md).
2. Name it with the next number, e.g. `0002-my-decision.md`.
3. Start it in `Proposed` status; move to `Accepted` once agreed.
4. Add it to the index table above.
