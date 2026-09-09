# SemReg issue #33 author evidence

## Scope

Base: `e1d4c70924254cf44f39304fc94856925328675d` (`main`).

Changed implementation-test file: `semreg/v1/publication_fork_test.go`.

The added direct regression invokes `Fork` on a nil `*PublicationKernel`. It
asserts a nil fork result, `InvalidValue` through `ErrorIdentifier`, and the
stable supported diagnostic subject `publication kernel`.

No production code, public API, transport, persistence, lifecycle, or gateway
behavior changed.

## Validation

`env GOWORK=off go test -count=1 -v ./semreg/v1 -run '^TestPublicationKernelFork'`

- PASS: 8 focused fork tests, including `TestPublicationKernelForkNilReceiver`.
- FAIL: 0.

`./scripts/ci_local.sh`

- PASS: 8 Go packages; the script completed its formatting, module, import,
  vet, race-test, and build gates with exit status 0.
- FAIL: 0.

`git diff --check`

- PASS: no whitespace errors.
