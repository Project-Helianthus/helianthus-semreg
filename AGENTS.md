# AGENTS

## Purpose and ownership

`helianthus-semreg` owns Project Helianthus protocol-neutral semantic code,
versioned capability packages, public interfaces and types, validation, and
compatibility fixtures.

Keep the core independent from transports, protocols, registries, gateways,
vendors, output bindings, and user interfaces. Native repositories retain
framing and I/O, protocol lifecycle, profile qualification, decoding, native
identity, and raw evidence. Gateway repositories retain runtime composition and
driver lifecycle orchestration. Consumers use reviewed promoted contracts and
must not define upstream semantics.

Preserve exact values and units, time domains, evidence and source lineage,
alternatives and conflicts, candidate-versus-qualified status, capability
withdrawal and generation, and explicit projection loss. Missing or unsupported
facts remain distinct from invalid, stale, or unavailable facts. Do not flatten
native capabilities into an implicit lowest common denominator.

## Working rules

1. Reconcile `origin/main`, the working tree, related issues, branches, pull
   requests, reviews, and checks before editing.
2. Use one scoped issue and an `issue/<number>-<slug>` branch created from the
   current `origin/main`. Keep unrelated work out of the branch.
3. Keep public packages and fixtures versioned. A compatibility change must name
   the affected contract version, migration behavior, consumer impact, and
   rollback boundary.
4. Do not add a protocol, vendor, gateway, consumer, sibling-checkout, or private
   repository dependency to semantic packages. Keep fixtures publishable and
   self-contained.
5. Run `./scripts/ci_local.sh` from a standalone clone before pushing. Add
   focused tests for low-risk changes and RED-first evidence when behavior,
   persistence, concurrency, lifecycle, operation routing, or safety benefits.
6. Open a linked pull request with scope, evidence, validation, documentation
   gate, and residual risk. Resolve valid P0-P2 findings, then obtain a fresh
   exact-HEAD `NO_BLOCKING_FINDINGS` review.
7. Squash merge only after applicable checks and required documentation are
   green. Verify remote `main`, issue, pull request, and branch state, then stop
   at the requested boundary.

## Documentation and evidence

Reusable protocol-neutral architecture and API documentation belongs in
[`Project-Helianthus/helianthus-docs-semantic`](https://github.com/Project-Helianthus/helianthus-docs-semantic).
Protocol-native facts and evidence stay in their public protocol documentation
owner. Semantic or public API behavior changes require a documentation issue and
reviewed contract before dependent code is accepted.

Every durable claim and fixture must identify publishable evidence and its
version. Mark hypotheses and unknowns explicitly. Never copy restricted
standards text, private captures, personal data, credentials, serials, network
coordinates, or device fingerprints into the repository.

## Gates and safety

Run the repository's complete build, vet, test, formatting, module, and import
checks. Apply protocol, transport, conformance, or physical smoke gates only
when a future scoped change actually affects those contracts; this repository
does not gain native I/O ownership through a semantic mapping.

Any credential handling, real installation, destructive or irreversible action,
safety-relevant control, or live-device write requires explicit operator
confirmation at action time. Reads and qualification must remain bounded,
version-aware, and fail-closed.
