# SemReg #38 immutable pack metadata v1

Base: `f3f761bc67e10d6a65eba6c13cb4dc51002d6955` (tree
`b55c25052e74615023c7b1ebf2e1329c0b2f1357`).

The implementation adds protocol-neutral `PackMetadata` records and an immutable
`PackMetadataRegistry` to `semreg/v1`. The registry provides the six accepted
read-only queries: `Packs`, `HasDefinition`, `CanonicalUnit`,
`ServiceOwnsCapability`, `FieldMatches`, and `OperationMatches`. All lookup keys
retain the complete typed `PackRef`/`DefinitionRef` tuple, while packs enumerate
in canonical UTF-8 byte order.

Each accepted Thermal/HVAC 1.0, PV 1.0, Storage/BMS 1.1, EVSE 1.0, and
Infrastructure 1.0 package exports a fresh record from the same private field,
service, capability, and operation tables used by its validator. The central
constructor validates the validator index, exact pack/version ownership, units,
fields, services, capabilities, operation tuple, duplicates, and ambiguous or
cross-pack relations before copying all source collections. Query results cannot
mutate the registry. Storage exports only `storage.state.soc` with `unit.percent`;
the two stale aliases are rejected.

The required public contract is docs-semantic PR #30, merged as
`2b3ca7835d2cef623ed6710e1c27a1ea73715aa2`; its reviewed worktree was
`d34b433e6d5af02062443765b763bac294b67c39` and the accepted independent report
has SHA-256
`c17d3cb528c1b2fe5893a9d6377794ca21aa923edb1ab091a58729ea7a616923`.

Validation:

- Focused normal and hostile metadata suite, including every field/service/
  capability combination and each operation tuple position: PASS.
- Focused race suite: `env GOWORK=off go test -race -count=1 ./semreg/v1 -run
  'TestPackMetadata'`: PASS.
- `env GOWORK=off go vet ./...`, `env GOWORK=off go build ./...`, and `git diff
  --check`: PASS.
- `./scripts/ci_local.sh`: PASS (format, module hygiene, imports, vet, complete
  race test suite, and build).

This change does not import Gateway, Portal, protocol, vendor, transport, or
consumer code. It neither publishes metadata at runtime nor admits operations,
adds compatibility paths, changes live/device behavior, or implements 0.8 code
generation. A fresh independent exact-HEAD review and hosted checks remain
required before merge.
