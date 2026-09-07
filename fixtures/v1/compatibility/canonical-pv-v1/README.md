# Canonical-PV v1 migration comparator fixtures

These immutable, test-only fixtures compare the retained
`helianthus.canonical-pv/v1` donor with `helianthus.semantic.kernel/v1` and
`helianthus.pack.pv@1.0.0`. They are compatibility evidence only: they create
no native mapping, route, authority, persistence, gateway output, or consumer
cutover.

`legacy-gateway-golden.json` is copied byte-for-byte from the pinned public
gateway fixture. It is a one-fact gateway-output witness, not the complete
corpus. `manifest.json` records its SHA-256 plus the exact mapper and mapper
test blobs. `comparator-vectors.json` is a separately labeled synthetic,
complete mapper witness with explicit counter/time/replay cases.
`dispositions.json` accounts for the mapper's exact fourteen source fields:
eight exact, three transformed, and three deliberately withheld. Every
non-exact row names its loss and legacy-path rollback treatment.

The fourteen identities are frozen in the `producer_requested_output_witness`
of `manifest.json`, alongside the public gateway producer and exercised-test
pins. Its withheld rows are
`inverter.ac.current.total`, `inverter.events.1`, and `inverter.events.2`.
Aggregate current is deliberately not synthesized; both event meanings remain
unknown. Each loss fails closed to `select_legacy_output`.

Reset, including an evidenced non-decreasing reset, and rollover vectors carry
their exact public evidence digest and required donor inputs. The comparator
mirrors the pinned donor validation,
rejects missing, malformed, and substituted evidence, and preserves accepted
digests as canonical SemReg `EvidenceRef` entries on the test-only energy
candidate provenance. Counter continuity itself remains compatibility evidence,
not a new semantic fact or runtime claim.

The comparator never treats a legacy asset reference as a route. The retained
legacy producer remains the rollback path; this fixture merely detects a future
shadow/cutover mismatch.
