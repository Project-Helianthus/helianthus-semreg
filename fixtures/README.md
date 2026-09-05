# Compatibility fixtures

This directory is the future home of versioned, public compatibility fixtures
for protocol-neutral semantic contracts.

The bootstrap contains no synthetic fixtures. A later implementation issue must
identify each donor contract and immutable source revision, specify the expected
semantic contract version, preserve native evidence and unknown values, and
record every accepted migration difference. Fixtures must run from a standalone
clone without private data, sibling repositories, network access, or live I/O.
