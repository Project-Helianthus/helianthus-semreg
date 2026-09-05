# helianthus-semreg

`helianthus-semreg` is the public Project Helianthus owner for protocol-neutral
semantic code, versioned capability packages, interfaces, types, and
compatibility fixtures.

## Bootstrap status

This repository currently contains ownership and validation scaffolding only.
It does not publish a semantic schema, runtime interpreter, stable API, native
mapping, or release-ready implementation. The first typed architecture and API
contract must be reviewed in
[`helianthus-docs-semantic`](https://github.com/Project-Helianthus/helianthus-docs-semantic)
before dependent implementation is accepted here.

The documentation-only package at `semreg/v1` reserves the first versioned
namespace and proves that the module builds independently. It exports no model
or runtime behavior. Independently versioned thermal, PV, storage, EVSE, and
infrastructure capability packages will be added only through later scoped
issues with accepted contracts and fixtures.

## Ownership boundary

This repository will own protocol-neutral identities, facts, relations,
capabilities, operations, projections, lifecycle contracts, and compatibility
fixtures. It will preserve native evidence, alternatives, conflicts, unknown
values, and explicit projection loss.

It does not own transport framing or I/O, protocol lifecycle, vendor/profile
qualification, native decoding or identity, raw evidence capture, gateway
composition, output bindings, or consumer policy. The semantic packages must
not import transport, protocol, registry, gateway, vendor, output, or UI
packages. Public builds and tests must work without sibling or private
repositories.

## Compatibility sources

The first implementation work must inventory and test its public migration
donors rather than copying them into this repository as universal semantics.
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

The check formats and builds the module with workspace discovery disabled,
rejects external module dependencies and non-module imports, and runs vet and
race-enabled tests. Protocol conformance and physical smoke gates do not apply
to this ownership-only bootstrap.

## License

This repository is licensed under the GNU Affero General Public License v3.0.
See [LICENSE](LICENSE).
