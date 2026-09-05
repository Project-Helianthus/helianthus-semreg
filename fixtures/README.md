# Compatibility fixtures

This directory is the future home of versioned, public compatibility fixtures
for protocol-neutral semantic contracts.

[`v1/foundation-vectors.json`](v1/foundation-vectors.json) copies the 30
foundation-relevant vector entries from the merged semantic v1 contract at
`Project-Helianthus/helianthus-docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac`.
Tests execute supported foundation properties and explicitly skip vector
operations that require the deferred snapshot, evaluation, or admission runtime.

Fixtures must run from a standalone clone without private data, sibling
repositories, network access, or live I/O. Future compatibility fixtures must
identify each donor contract and immutable source revision, preserve native
evidence and unknown values, and record every accepted migration difference.
