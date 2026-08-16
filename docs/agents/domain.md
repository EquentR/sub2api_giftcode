# Domain Docs

This repository uses a single-context domain documentation layout.

## Before exploring

- Read `CONTEXT.md` at the repository root when it exists.
- Read relevant decisions under `docs/adr/` when that directory exists.
- If either location is absent, proceed without treating the absence as an error.

## Layout

```text
/
├── CONTEXT.md
└── docs/
    └── adr/
```

`CONTEXT.md` holds the domain glossary and stable terminology. `docs/adr/` holds architectural decisions that are difficult to reverse, surprising without context, or based on an important trade-off.

## Vocabulary

Use terms exactly as defined in `CONTEXT.md` in issues, specifications, tests, and implementation discussions. When a needed concept is missing, reconsider whether it is implementation language or note it for later domain modeling.

## Decision conflicts

If proposed work conflicts with an existing ADR, surface the conflict explicitly rather than silently overriding the decision.
