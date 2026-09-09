# SemReg #35 sequential lifecycle fix

Base: `16e54e94fff023263d24c18c685efdfb35745b04`.

Preserves an immutable retained observation with `removal=generation_fence`
when its original binding later transitions from fenced to retired. Validation
requires both the original matching generation fence and the matching retired
source descriptor; it does not rebind or rewrite the retained candidate.

Validation:

- Focused normal: `TestRetainedObservationFenceThenRetirementPreservesRemoval` PASS.
- Focused race: SHA-256 `53bfe4eaa44a5e1ce4f0d9f0cabeefa34422d906910db498673f86fcc9ec8bfa`.
- `./scripts/ci_local.sh`: PASS; SHA-256 `13632b53b5b075d9e7a8314131c43195caf0bc7b1b65e1ae216a7fae9a308fd2`.
