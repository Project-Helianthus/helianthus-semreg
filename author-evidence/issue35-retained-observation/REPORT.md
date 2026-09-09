# SemReg issue #35 author evidence

## Scope

Base: `c8d3003071d22de436b6364c260da44910fe4af6` (`main`).

This change adds protocol-neutral `RetainedObservationRecord` state to a
snapshot and the corresponding evaluation records. On a generation fence or
source-epoch retirement, publication copies an otherwise removed observed fact
into that state without changing its original candidate, value, times,
evidence, binding, epoch, generation, or freshness policy. Current facts and
native lifecycle state continue normally. Evaluation exposes only unexpired
retained observations, while selection and operation admission consume current
facts only; a retained precondition is rejected as `precondition_failed`.

Every retained record has a digest-bound `retention_id` derived from its exact
immutable observation. This permits repeated lifecycle transitions and a
current replacement to reuse a stable candidate ID without rebinding, losing,
or making historical records operation-eligible. An explicit withdrawal removes
matching retained records only after source/lifecycle ownership validation.
`CurrentAt` performs deadline expiry from caller-supplied time even during a
publication gap, resealing an updated snapshot and leaving ordinary `Current`
as a detached read of that latest committed state.

Retained state is not a gateway cache, parallel store, rebinding mechanism, or
route/authority fallback.

## Focused validation

`env GOWORK=off go test -count=1 -v ./semreg/v1 ./semreg/v1/operation`

- PASS: 152 PASS entries, including same-ID and repeated replacements,
  retained withdrawal, canonical ordering/duplicates, no-publication
  explicit-time expiry, rejection-non-advance, and operation admission.
- FAIL: 0.
- Log SHA-256: `81d1744865708eaa90d1f964a13154be48213572e3140f92e30aa9cc6702c5f2`.

`env GOWORK=off go test -race -count=1 ./semreg/v1 ./semreg/v1/operation`

- PASS: core in 64.041s and operation in 11.631s; no race reports.
- FAIL: 0.
- Log SHA-256: `b5c53010a730bc32d56646382a5713de9195428499bbc0f02cb0c17cb736fc28`.

`./scripts/ci_local.sh`

- PASS: 8 Go packages; formatting, module tidy/diff, import boundary, vet,
  race test, and build gates completed with exit status 0.
- FAIL: 0.
- Log SHA-256: `58eb2f63b92574d29cfd8824c81271dba67d492af48dc322efac95fe771d0431`.

`git diff --check`

- PASS: no whitespace errors.

## Residual risk

Expiry is evaluated against caller-supplied time through `CurrentAt` and during
publication. The separately owned public documentation gate is pending at
`helianthus-docs-semantic#22`; this PR must not merge until that contract is
reviewed. No persistence, gateway projection, native lifecycle lock, or
live-device behavior is introduced or claimed.
