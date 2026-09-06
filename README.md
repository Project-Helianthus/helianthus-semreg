# helianthus-semreg

`helianthus-semreg` is the public Project Helianthus owner for protocol-neutral
semantic code, versioned capability packages, interfaces, types, and
compatibility fixtures.

## Storage/BMS capability pack v1.1

`semreg/v1/packs/storage` implements the immutable
`helianthus.pack.storage@1.1.0` validator from accepted semantic docs revision
`c20fdef`. It indexes 20 fields, seven services, five capabilities, and two
charge/discharge limit operation/effect pairs. Each operation requires one
qualified-current, same-interface, exact known-clear storage interlock
precondition before the existing operation kernel evaluates authority or any
dispatch boundary. The pack has no native mapping, route, authority, Portal
composition, persistence, gateway, consumer, or live-I/O behavior; eeBUS
remains `unknown_pending_std_01`.

## EVSE capability pack v1

`semreg/v1/packs/evse` implements the immutable
`helianthus.pack.evse@1.0.0` validator from
[`helianthus-docs-semantic@4fc5661`](https://github.com/Project-Helianthus/helianthus-docs-semantic/tree/4fc56613a521dd2549cc81b1178f248dbcfd5164/api/v1/packs).
It indexes 15 field contracts, six static dimensions, five services, six
capabilities, one allocated-current operation/effect pair, five static
relationships, and six Portal descriptors. It validates exact qualified,
available capability constraints and exact allocated-current argument matching
through the existing operation kernel; it does not implement topology
instances, native mappings, Tesla sending or routes, eeBUS qualification,
Matter conformance, Portal composition, consumer behavior, persistence, or
live I/O. Tesla remains a no-sender/no-route/no-live-authority candidate,
eeBUS remains `unknown_pending_std_01`, and Matter is input-only. Native
counter reset/wrap evidence, lifecycle and loss semantics remain outside this
pack. Capability-to-service instance pairing remains static metadata because
the current `PackValidator` boundary sees only an opaque service-instance ID.

## Thermal/HVAC capability pack v1

`semreg/v1/packs/thermal` implements the immutable
`helianthus.pack.thermal@1.0.0` validator from
[`helianthus-docs-semantic@4fc5661`](https://github.com/Project-Helianthus/helianthus-docs-semantic/tree/4fc56613a521dd2549cc81b1178f248dbcfd5164/api/v1/packs).
It indexes 13 field contracts, five services, seven capabilities, two bounded
operation/effect pairs, and static Portal descriptors. It validates semantic
facts and pack-owned thermal effects through the existing operation kernel; it
does not implement native mappings, routing, dispatch, Portal composition,
consumer behavior, persistence, or live I/O. Capability-to-service definition
pairing remains static metadata because the current PackValidator boundary sees
only an opaque service-instance ID.

## Infrastructure capability pack v1

`semreg/v1/packs/infrastructure` provides the immutable read-only validator for
`helianthus.pack.infrastructure@1.0.0`, accepted from
[`helianthus-docs-semantic@4fc5661`](https://github.com/Project-Helianthus/helianthus-docs-semantic/tree/4fc56613a521dd2549cc81b1178f248dbcfd5164/api/v1/packs).
It indexes 19 electrical facts, six services, and six read capabilities. It
checks canonical text dimensions, units, Decimal bounds, and status symbols
through the existing semantic registry only. It indexes no operations or effect
rules and exports no native mapping, route, Portal, consumer, topology-instance,
or control API. The copied machine fixture pins the source contract and static
relationship/Portal metadata; passing its tests is not a native conformance,
mapping qualification, live-I/O, or hardware claim.

## Implemented v1 foundation, publication, evaluation, selection, operations, and projection

The `semreg/v1` package implements the typed BASE foundation against the accepted
[`helianthus-docs-semantic` contract at da5ab44](https://github.com/Project-Helianthus/helianthus-docs-semantic/tree/da5ab4415d3bec73f9572aec1c495a6cdcbcba47/api/v1).
It provides:

- protocol-neutral identities, exact values, evidence, lineage, time, quality,
  fact candidates/envelopes/conflicts, definitions, services and capabilities;
- record `Validate` methods and stable rejection IDs through `ErrorIdentifier`;
- `NewRegistry` with exact pack and definition ownership and typed validator
  hooks;
- strict recursive `Decode[T]` and convenience decoders, plus `CanonicalJSON`
  and `DigestRecord` for supported valid records; and
- `PublicationKernel`, which applies canonical `PublicationBatch` patches
  atomically and returns deep-copied immutable `Snapshot` values and bytes with
  exact revision, replay, lifecycle-fence, dependency-cascade and conflict
  behavior;
- pure `EvaluateSnapshot`, which creates a complete, digest-bound time view
  from an explicit context, including conservative restart-time uncertainty and
  transitive derivation aging; and
- `SelectionKernel`, which registers exact presentation-policy ID/version
  pairs and returns immutable, snapshot/view-bound presentation-only results;
  and
- `semreg/v1/operation`, which provides the accepted v1 intent, precondition,
  capability requirement, admitted route, dispatch, acknowledgement, readback,
  execution-record and outcome shapes; exact operation-pack validation and
  effect-hook resolution; immutable snapshot admission; exact source epoch,
  generation, route and capability-revision guard claims for the external
  native owner; append-only terminal evidence; and intent/outcome idempotency
  with configurable bounded, fail-closed in-memory retention without claiming
  durable native replay safety.
- `semreg/v1/projection`, which provides the accepted v1 projection manifest,
  requested-item, loss, disposition, snapshot/revision-bound report, and
  compatibility-alias records. `Project` is a pure detached report constructor:
  it validates complete tuple accounting and explicit loss/reason semantics but
  creates no semantic facts, capabilities, authority, intents, bindings, or
  routes. Compatibility aliases remain non-routable.

`NewKernelWithOptions` selects the bounded in-memory idempotency capacity;
`RecordRejection` retains structurally valid rejected intent bytes without
re-running a pack hook that already rejected them. `AdmitSelectionRoute` and
`AdmitAliasRoute` are explicit fail-closed public boundaries for the two
accepted negative operation cases. They do not implement selection routing,
alias storage, or native dispatch.

Typed operation inputs are validated before detached copies or hook dispatch,
so malformed unions and invalid UTF-8 cannot be repaired by serialization or
installed in retained state. Admission and execution validation collect and
rank independently knowable failures using the accepted stable precedence.
Both record entry paths validate supplied evidence before returning an input
error. Readback hooks require exact binding to the kernel's owned admission
and a safely resolved current route and candidate. `DecodeIntent` and
`DecodeExecutionRecord` classify absent or malformed document contracts as
`invalid_contract`, preserving strict JSON and duplicate-key precedence.
Readback keeps common exact snapshot, route, generation, eligibility, and
pack-effect checks for every supported outcome; only `applied` requires strict
post-completion receipt proof, while `conflict` requires post-dispatch evidence
and `acknowledged_unverified` may retain same-route inconclusive evidence.

The self-contained fixture runners execute the copied K-POS-005 restart-time
vector plus the exact corrected K-POS-019/K-POS-025 and K-NEG-056 through
K-NEG-065 inputs from docs issue #6, alongside the foundation and publication
fixtures. They also pin and execute all 29 operation-tagged acceptance vectors
from the same accepted docs commit using a duplicate-key-rejecting loader,
exact stable result/error assertions, immutable rejection checks, and visible
typed-expansion digests. The typed expansions preserve admitted-to-readback
candidate revisions, replacement generations, effective stale state, complete
fact-owner registration, causal lifetimes, and receiver ingress/sender egress
transitions. The executable cases exercise authority, time, causal,
precondition, route, generation, pack-owner, effect, idempotency, dispatch,
acknowledgement, readback and outcome requirements through a contract-derived
matrix. Exact typed EVSE and precondition expansions supplement the common
harness. The subsets are pinned to the accepted docs commit and exact source-file
SHA-256. Passing these checks does not establish all runtime acceptance
criteria.

Production capability packs and mappings, normative Matter/eeBUS/native
dispositions, Portal/gateway/consumer composition, persistence and migrations
remain follow-on work. This base projection/compatibility increment does not
establish INT-06 lifecycle ownership, full INT-05 completion, a software 0.7
release, hardware readiness, physical validation, release-level security review
or software-release acceptance.

## Ownership boundary

The repository's ownership includes protocol-neutral semantic contracts and
compatibility fixtures. The implementation preserves native evidence,
alternatives, conflicts and unknown values; operation and projection runtime
remain separated from native transport and gateway ownership.

It does not own transport framing or I/O, protocol lifecycle, vendor/profile
qualification, native decoding or identity, raw evidence capture, gateway
composition, output bindings, or consumer policy. The semantic packages must
not import transport, protocol, registry, gateway, vendor, output, or UI
packages. Public builds and tests must work without sibling or private
repositories.

INT-06 owns the native lifecycle lock, immediate current-generation and fence
recheck against the admitted guard claims, native callback, handle, and release
lifecycle. Semreg neither acquires that lock nor invokes or owns a native
callback or handle.

## Compatibility sources

Future migration work must inventory and test its public migration donors
rather than copying them into this repository as universal semantics.
Reviewed starting points include:

- [`helianthus-ebusreg@738142f`](https://github.com/Project-Helianthus/helianthus-ebusreg/tree/738142f97519b3d5566f6d75d9c3975d7ffe2e96),
  including the current eBUS/PV value, lifecycle, registry, and projection
  contracts;
- [`helianthus-ebusgateway@e31106c`](https://github.com/Project-Helianthus/helianthus-ebusgateway/tree/e31106c9c726fbb8df7546901763e19b93659e72),
  including the existing DriverManager and native-to-canonical adapter seams;
- [`helianthus-eebusreg@ede40b9`](https://github.com/Project-Helianthus/helianthus-eebusreg/tree/ede40b929e9c2d9cbf0de20c4844e737d5e6af68),
  including native snapshots and evidence envelopes;
- the [`eeBUS normative source ledger@81cd647`](https://github.com/Project-Helianthus/helianthus-docs-eebus/blob/81cd647c834e88c88a3c82ef9fbc5a0194f6b0f1/protocols/eebus-normative-source-ledger.md),
  whose unresolved revisions remain unresolved.

These revisions are compatibility inputs, not claims that their contracts are
protocol-neutral or that current normative mappings are complete.

## Development

Run the complete local check from a standalone clone:

```sh
./scripts/ci_local.sh
```

The check verifies formatting, module tidy/diffs and declared dependency/import
boundaries, then runs vet, race-enabled tests and build with workspace discovery
disabled. The sole direct external dependency is `golang.org/x/text v0.22.0`
for NFC validation; the script permits its explicit module graph and rejects
undeclared modules and imports outside this module or x/text. The foundation is
checked with Go 1.22. Native protocol conformance and physical smoke gates do not
apply to this protocol-neutral BASE implementation.

## License

This repository is licensed under the GNU Affero General Public License v3.0.
See [LICENSE](LICENSE).
