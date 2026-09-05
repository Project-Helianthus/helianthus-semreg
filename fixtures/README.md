# Compatibility fixtures

This directory contains versioned, public compatibility fixtures
for protocol-neutral semantic contracts.

[`v1/foundation-vectors.json`](v1/foundation-vectors.json) copies the 30
foundation-relevant vector entries from the merged semantic v1 contract at
`Project-Helianthus/helianthus-docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac`.
Tests execute 26 foundation vectors, including the implemented BASE publication
and snapshot behavior. Four vector operations remain deferred: K-POS-005,
K-NEG-014, K-NEG-015, and K-NEG-048. Evaluation, presentation selection,
operations, and projection runtimes remain separate work; these fixtures do not
claim INT-05 or full runtime completion.

[`v1/publication-vectors.json`](v1/publication-vectors.json) pins deterministic
publication scenarios to the same accepted documentation revision. The matching
Go fixtures exercise initial publication, patch retention, withdrawals, replay
and conflict rejection, source retirement, generation supersession, graph
cascade, conflict reconciliation, bounds, dangling references and deep-copy
isolation.

Fixtures must run from a standalone clone without private data, sibling
repositories, network access, or live I/O. Future compatibility fixtures must
identify each donor contract and immutable source revision, preserve native
evidence and unknown values, and record every accepted migration difference.
