---
title: project
status: active
desc: Project-level contract for scope, spec ownership, and source-of-truth rules.
---
# project

## raw source
The project provides a short-video feed system with accounts, videos, social interactions, a coin wallet for recharge and tipping, invoices for paid recharge, asynchronous processing, and web access.

## expanded spec
The project contract is behavior-first. It describes what the product must preserve and assigns detailed ownership to the backend, frontend, and leaf specs without duplicating their implementation details.

The spec tree follows these ownership rules:

- `project/spec.md` defines the global scope and the rules for maintaining the tree.
- `backend/spec.md` and `frontend/spec.md` define the architecture boundary of each runtime package.
- A leaf spec defines one business or user-facing capability and maps its primary implementation file or files through `code`; supporting files use `related`. The same source file may appear in multiple specs only when it implements distinct behavior boundaries, and each spec must state its own contract.
- An `eval.md` file records observable behavior scenarios. It is evidence for a contract, not a second implementation description.

Deployment instructions remain in the public `README.md` and `docker-compose.yaml`. Credentials, signing keys, and other environment-specific values remain outside the repository and are never copied into a spec.

## change rules
When a user-visible behavior changes, update the owning behavior spec and its `eval.md`. When a change crosses the backend/frontend boundary, update every affected owner. Structural parent specs may omit `eval.md` when their contract is covered by child scenarios or integration checks. A refactor that preserves behavior does not require new scenarios, but source mappings must be updated if the implementation boundary changes.
