# helianthus-semreg

`helianthus-semreg` is the public Project Helianthus owner for protocol-neutral
semantic code, versioned capability packages, interfaces, types, and
compatibility fixtures.

## Capability packs

Capability packs are read-only at run time: no publication, evaluation or
operation changes a pack definition. Until the Helianthus 0.7 release, pack
definitions and their copied fixtures change in place, without a new pack or
contract version. A pack declares relation definitions only. Relation instances
are kernel records defined by the
[graph contract](https://github.com/Project-Helianthus/helianthus-docs-semantic/blob/main/api/v1/graph.md);
their implementation follows in
[#90](https://github.com/Project-Helianthus/helianthus-semreg/issues/90).

## Element catalog

`semreg/v1/graph` holds the element catalog of the
[graph contract](https://github.com/Project-Helianthus/helianthus-docs-semantic/blob/main/api/v1/graph.md):
six planes, seven core relation types, the identification schemes, the lenses
and one entry per semantic element with its owner, disposition, planes and
admission record. Its source is the byte copy
[`fixtures/v1/element-catalog-v1.json`](fixtures/v1/element-catalog-v1.json),
pinned by SHA-256 in `graph.PinnedDigest`.

| Function or method | What it does |
|---|---|
| `Decode` | Strictly decodes `helianthus.semantic.element-catalog/v1`: no repeated, unknown or missing member, and every value in its vocabulary. |
| `Check`, `Validate` | Apply CAT-1 to CAT-9 against the metadata and definition indexes of the given packs; `Validate` returns the first finding as a stable error. |
| `Load` | Accepts only the pinned bytes, then decodes and validates them against the seven accepted packs. |
| `Catalog` lookups | Entry by exact `DefinitionRef` or core `DefinitionID`; planes and disposition of an entry; kind of a dimension and a service; core type, orientation, endpoint kinds, cardinality, basis classes and medium of a relation definition; identification schemes and lenses. Every result is a detached copy. |
| `AssembleCut` | Reads at most 256 snapshots of distinct assets together as one graph cut, with a caller-asserted `complete` or `partial` completeness. It rejects only invalid input: a snapshot that fails validation, two snapshots of one asset (`duplicate_key`) or more than 256 (`bounds_exceeded`). The cut identifier is `sha256:` over the JCS bytes of `{contract:"helianthus.semantic.graph-cut/v1", members}`, the `(asset_id, snapshot_id)` vector sorted by asset; there is no global revision. |
| `Cut` findings and coherence | `duplicate_identity` excludes both members and makes the cut `partial`; `same_unit`, `contradicted_absence`, `anchor_parent_unresolved` and `ungraphed` are reported without blocking the read. Every relation instance of a kept member is `coherent`, `stale`, `dangling`, `outside_cut`, `excluded` or `conflict`, including cardinality, forest and cycle violations across members and a far end of a kind the definition does not admit. |
| `Cut.PlanesOfResource`, `PlanesOfRelation`, `PlanesOfFact` | Plane membership over the cut (PLN-2, PLN-3): a relation instance only when qualified and coherent, a fact, service or capability when its definition is a member and its subject is current, and a current resource only in the planes its kind admits. |
| `Cut.CanonicalJSON` | The one encoder of the cut's output: members by asset, findings by kind and canonical subject, relation instances by canonical definition, `from` and `to`; identical inputs give identical bytes. |
| `CutStore` | Retains the newest cut and every cut an unexpired page token names, at most 8 cuts and at most 120 seconds after a cut stops being the newest (CUT-3). A pin the store does not retain is `revision_conflict`. The store is the only state. |
| `ListResources` | Lists the current resources of one cut, optionally by kind and plane, in resource id order, in pages of at most 4096 (ENT-2). Every page carries the served cut. |
| `Traverse` | Walks qualified coherent instances from one entry (a current resource, the roots of a lens, or installation sites), by a lens or by planes and an optional set of relation definitions, in one direction and to a bounded depth (ENT-1, TRV-1 to TRV-8). Planes alone follow every member relation of those planes. A resource reached twice is listed once, at its smallest depth. Depth and fan-out stop with frontier entries and counts. A page token is bound to the cut and the request. |
| `RetrieveLens` | Retrieves one lens over a retained cut: the resources reached, the relation instances among them, the absence statements, the facts, services and capabilities in the lens planes, the cut findings and the frontier (LRS-1 to LRS-6). A lens whose roots are absent returns an empty result and the cut reference. |
| `SumMembers` | Sums the member facts of one definition on the resources reached from a scope. A sum that cannot be formed is withheld with a reason and is never a partial total (SUM-1 to SUM-6). |

The package imports the root package and the pack packages only, and neither
the root package nor the operation package imports it. It records membership
and admission, reads snapshots together, and serves a listing, a bounded
traversal and a lens retrieval of one cut. Those reads are pure functions of
the cut. `CutStore` is the only state. The package publishes, evaluates and
admits nothing. There is no pack availability view.

### Graph conformance

The implementation conforms to the graph contract only when every positive
and negative vector of
[`fixtures/v1/graph-acceptance-vectors.json`](fixtures/v1/graph-acceptance-vectors.json)
passes with the element catalog registered, and every kernel vector passes
without it. The two commands are:

```sh
GOWORK=off go test -count=1 -v -run 'TestGraphAcceptanceVectors' ./semreg/v1/graph/
GOWORK=off go test -count=1 ./semreg/v1/ ./semreg/v1/operation/ ./semreg/v1/projection/
```

The first runs every graph vector by identifier through `construct_registry`,
`register_catalog`, `apply`, `assemble_cut`, `admit`, `list_resources`,
`traverse`, `retrieve_lens`, `sum_members` and `project` on the public API; no
vector is skipped or selected by identifier, and a vector whose operation the
harness does not implement fails. The harness completes the abbreviated
records of the document only by adding absent members (the kernel contract,
times, empty required collections, a source descriptor for a new source epoch,
a fact candidate's time axes and freshness policy, derivation source paths
read from the referenced candidate, the batch digest, an intent's
arguments, causal context, revisions, idempotency key and deadline, and a
read snapshot's contract, revisions, evaluation time, synthetic source and
the origin, revision and anchor evidence it omits). A present empty graph
collection is omitted, which is the kernel wire form. The harness then
decodes the records with the kernel's strict decoders. The second runs the
kernel vector copies, which carry no graph record. `TestOperationDoesNotImportGraph`
in `semreg/v1/operation` checks that admission cannot read the graph (AUT-1,
ACT-4). `semreg/v1/projection` imports no pack.

Each vector ends in one of three outcomes. It passes; it fails, for an
operation or assertion the harness does not implement or for any outcome the
vector does not expect; or it ends as a document finding. A document finding
is recorded only when the harness, before and independently of the kernel,
finds that a record of the document breaks a named contract rule and the
kernel refuses that record with exactly the stable error and detail of that
rule. A refusal no rule explains fails, and so does a record that breaks a
rule but is accepted. The test log lists every finding by rule and vector.

Conformance is not reached yet: it requires every vector to pass. 86
vectors execute and none are skipped. 6 pass: `G-NEG-ACT-2`, `G-NEG-CAT-1`,
`G-NEG-CAT-2`, `G-POS-ACT-1`, `G-POS-CAT-1` and `G-POS-CAT-2`. None fail.
The copied document records the element-catalog digest of the seed it was
written against. `catalog_digest_exposed` and `digest_equals_pinned_digest`
require the loaded catalog digest to equal `graph.PinnedDigest`; the recorded
citation is not an input to either assertion. The pinned catalog is unchanged.
The other 80 vectors end as document findings: relation definitions are
written as owner-tagged catalog references instead of a definition reference
(35); operator origins carry native source fields (33); decimals are not
canonical (2); snapshot identifiers digest an empty retained-observation list
the kernel omits (3); and a read snapshot's identifier is of the abbreviated
object rather than the kernel snapshot (7). Each finding is resolved by an
in-place edit of the vector document, of the contract, of the kernel or of
the catalog copy; behind the first refusal further disagreements may remain,
and they surface as findings or failures once the first is resolved.

## Storage/BMS capability pack v1.1

`semreg/v1/packs/storage` implements the read-only
`helianthus.pack.storage@1.1.0` validator from accepted semantic docs revision
`c20fdef`. It indexes 20 fields, seven services, five capabilities, and two
charge/discharge limit operation/effect pairs. Each operation requires one
qualified-current, same-interface, exact known-clear storage interlock
precondition before the existing operation kernel evaluates authority or any
dispatch boundary. The pack has no native mapping, route, authority, Portal
composition, persistence, gateway, consumer, or live-I/O behavior; eeBUS
remains `unknown_pending_normative_source`.

## PV/inverter capability pack v1

`semreg/v1/packs/pv` implements the read-only
`helianthus.pack.pv@1.0.0` validator from accepted semantic documentation. It
indexes 24 fields, six services, eight capabilities, and the active-power and
export limit operation/effect pairs. It preserves the fixture's native mapping
and live-authority exclusions and adds no transport, gateway, persistence, or
live-I/O behavior.

## EVSE capability pack v1

`semreg/v1/packs/evse` implements the read-only
`helianthus.pack.evse@1.0.0` validator from
[`helianthus-docs-semantic@4fc5661`](https://github.com/Project-Helianthus/helianthus-docs-semantic/tree/4fc56613a521dd2549cc81b1178f248dbcfd5164/api/v1/packs).
It indexes 16 field contracts, six static dimensions, five services, six
capabilities, one allocated-current operation/effect pair, five static
relation definitions, and six Portal descriptors. It validates exact qualified,
available capability constraints and exact allocated-current argument matching
through the existing operation kernel; relation instances are kernel records,
and the pack does not implement native mappings, Tesla sending or routes,
eeBUS qualification, Matter conformance, Portal composition, consumer
behavior, persistence, or live I/O. Tesla remains a no-sender/no-route/no-live-authority candidate,
eeBUS remains `unknown_pending_normative_source`, and Matter is input-only. Native
counter reset/wrap evidence, lifecycle and loss semantics remain outside this
pack. Capability-to-service instance pairing remains static metadata because
the current `PackValidator` boundary sees only an opaque service-instance ID.

## Thermal/HVAC capability pack v1

`semreg/v1/packs/thermal` implements the read-only
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

`semreg/v1/packs/infrastructure` provides the read-only validator for
`helianthus.pack.infrastructure@1.0.0`, accepted from
[`helianthus-docs-semantic@4fc5661`](https://github.com/Project-Helianthus/helianthus-docs-semantic/tree/4fc56613a521dd2549cc81b1178f248dbcfd5164/api/v1/packs).
It indexes 20 electrical facts, seven services, and six read capabilities. It
checks canonical text dimensions, units, Decimal bounds, and status symbols
through the existing semantic registry only. It indexes no operations or effect
rules and exports no native mapping, route, Portal, consumer, or control API;
relation instances are kernel records. The copied machine fixture pins the
source contract and static relation/Portal metadata; passing its tests is not a
native conformance, mapping qualification, live-I/O, or hardware claim.

## Implemented v1 foundation, publication, evaluation, selection, operations, and projection

The `semreg/v1` package implements the typed BASE foundation against the accepted
[`helianthus-docs-semantic` contract at da5ab44](https://github.com/Project-Helianthus/helianthus-docs-semantic/tree/da5ab4415d3bec73f9572aec1c495a6cdcbcba47/api/v1).
Retained observations use the accepted sequential-lifecycle correction from
docs-semantic #22 / PR #24: reviewed
`b5cb2a8df6c0c94268f35476d62e36643767a8c3`, public main
`ed33276cddb2dd86757efcf335c95936bdf4efe2`, tree
`86c512ff154e2656b33c30633029bbc7cf3c4703`.
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
- explicit retained observations, which preserve an immutable original native
  observation after its exact binding is fenced or source epoch retired. They
  retain the original value, times, evidence, source path and freshness policy
  only until that policy's original deadline; evaluation exposes them as
  `retained_observations`, while current facts, lifecycle and routes continue
  independently. Retained observations are read-only historical state and are
  never selection or operation authority. Retained identity and canonical order
  use `(candidate_id, candidate_revision, JCS(key), binding_id,
  source_epoch_id, driver_generation)`, allowing a stable candidate ID to have
  current and historical instances. `CurrentAt` is read-only: it returns the
  unchanged audit snapshot bytes with its explicit-time evaluation view, which
  omits expired retained entries during a publication gap;
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
  routes. Compatibility aliases remain non-routable. The package holds target
  records and imports no pack (TGT-6). The pack availability view is retired;
  nothing in this package replaces it. Availability of an element is read
  from a lens result, and an asset outside the graph is `ungraphed` on the cut.

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
Snapshot-wide pack validation checks each capability's structure only;
qualification and availability are judged on the required route. A fenced or
retired generation's withdrawn capability tombstone therefore never refuses its
successor's admission or readback, while any intent or evidence naming the
retired route stays refused. Both record entry paths validate supplied evidence before returning an input
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

The test-only
[`canonical-PV v1 comparator`](fixtures/v1/compatibility/canonical-pv-v1/)
pins the public legacy donor, mapper/test sources, and a one-fact gateway golden
witness, then compares a separately identified complete mapper witness through
the real publication, evaluation, PV validation, canonical JSON and projection
APIs. It accounts for fourteen requested outputs (eight exact, three
transformed, and three withheld) with explicit loss and rollback treatment.
It does not add a native mapping, route, authority, persistence, gateway
composition, consumer behavior, or live-I/O path.

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
