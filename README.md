# helianthus-semreg

`helianthus-semreg` is the public Project Helianthus owner for protocol-neutral
semantic code, versioned capability packages, interfaces, types, and
compatibility fixtures.

## Implemented v1 foundation, publication, evaluation, and selection

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
  pairs and returns immutable, snapshot/view-bound presentation-only results.

The self-contained fixture runners execute the copied K-POS-005 restart-time
vector plus the exact corrected K-POS-019/K-POS-025 and K-NEG-056 through
K-NEG-065 inputs from docs issue #6, alongside the foundation and publication
fixtures. The new subset is pinned to the accepted docs commit and its exact
source-file SHA-256. Passing these checks does not establish all runtime
acceptance criteria.

Operation/projection/admission runtime, capability-pack catalogs/mappings,
persistence, migrations and integrations remain deferred.
This increment does not establish INT-05 completion, hardware validation,
release-level security review or software-release acceptance.

## Ownership boundary

The repository's ownership includes protocol-neutral semantic contracts and
compatibility fixtures. The implementation preserves native evidence,
alternatives, conflicts and unknown values; operation and projection runtime
remain future scoped work.

It does not own transport framing or I/O, protocol lifecycle, vendor/profile
qualification, native decoding or identity, raw evidence capture, gateway
composition, output bindings, or consumer policy. The semantic packages must
not import transport, protocol, registry, gateway, vendor, output, or UI
packages. Public builds and tests must work without sibling or private
repositories.

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
