# AGENTS.md

## Methodologies
- KISS — choose the simplest, most maintainable in the long term solution or workflow that works.
- DRY — reuse existing code, tests, and patterns before adding new ones.
- TDD — for meaningful behavior changes, mandatory failing test → implementation → verification; skip or adapt for docs-only, pure refactors, prompt/config edits, or operational work.
- RPI — for non-trivial coding work, resolve unknowns before implementation: perform codebase discovery → factual research → deep strategy; then make a concrete plan, implement, and verify. Ask the user to review the plan when product, architecture, security, or priority decisions remain open.

## Hazards
- Milvus-facing changes are cross-project changes: this repo talks to a custom Milvus-compatible Cloudflare Worker backend, so client/API changes must stay in lockstep with that worker.
- If testing the built binary instead of `go run .`, rebuild first; stale `bin/cfmantic-code` is a common false signal.
- Prefer mockery-generated mocks from `internal/mocks`; if a mock is missing, update `.mockery.yml` and regenerate instead of adding package-local handwritten test mocks.

## Pre-Commit Checklist
- Run `make pre-commit` after code changes.
- If validating the built binary instead of `go run .`, run `make build` first.
