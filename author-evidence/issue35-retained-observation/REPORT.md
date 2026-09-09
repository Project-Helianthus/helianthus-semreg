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

An explicit withdrawal remains a withdrawal. Retained state is not a gateway
cache, parallel store, rebinding mechanism, or route/authority fallback.

## Focused validation

`env GOWORK=off go test -count=1 -v ./semreg/v1 ./semreg/v1/operation`

- PASS: 150 named top-level tests, including generation and epoch-retirement
  retained-path, expiry, canonicality, rejection-non-advance, and operation
  admission witnesses.
- FAIL: 0.
- Log SHA-256: `ca853d51694b03bfe701f0e64cf17fb53c858e54d4f4b8c34147b669560e80c3`.

`env GOWORK=off go test -race -count=1 ./semreg/v1 ./semreg/v1/operation`

- PASS: core in 64.041s and operation in 11.631s; no race reports.
- FAIL: 0.
- Log SHA-256: `09b48fc7e8f3fe640b3f2519907b8a88652e7361c5779d34e69cda6138d3a81d`.

`./scripts/ci_local.sh`

- PASS: 8 Go packages; formatting, module tidy/diff, import boundary, vet,
  race test, and build gates completed with exit status 0.
- FAIL: 0.
- Log SHA-256: `4cfe18bb955c98f6e4813212e99da3c7d57a7d7aa3e8d0151ca31fe72c892873`.

`git diff --check`

- PASS: no whitespace errors.

## Residual risk

Expiry is evaluated against caller-supplied time and also pruned on a later
publication. No persistence, gateway projection, native lifecycle lock, or
live-device behavior is introduced or claimed.
