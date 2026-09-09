# SemReg #35 sequential lifecycle fix-forward

Base: `16e54e94fff023263d24c18c685efdfb35745b04`.

The runtime preserves an immutable retained observation with
`removal=generation_fence` when its original binding later transitions from
fenced to retired. Validation requires the original matching generation fence
and the matching retired source descriptor. It does not rebind, rewrite the
removal event, create parallel retained state, or make retained evidence
operation-eligible.

The correction imports the accepted docs-semantic issue #22 / PR #24 fixture
byte-for-byte from reviewed source
`b5cb2a8df6c0c94268f35476d62e36643767a8c3`, public main
`ed33276cddb2dd86757efcf335c95936bdf4efe2`, tree
`86c512ff154e2656b33c30633029bbc7cf3c4703`. The fixture SHA-256 is
`f98d57912a3a2f08291a80e6a65d8ecdd66a1037b7a580d3ec43b3ef1172ef73`.
Its loader pins all 19 ordered scenarios (9 positive, 10 negative). Runtime
regressions cover a single fence followed by retirement, multiple fenced
generations followed by retirement, exact retirement replay, and rejection
without state advance for foreign source, epoch, generation, and a later
distinct retirement. Unknown or foreign retirement epochs now return the
public contract's stable `stale_source_epoch` classification; existing error
precedence controls were updated to retain higher-ranked structural failures.

Validation:

- Focused normal retained-vector and sequential-lifecycle suite: PASS.
- Focused race suite: PASS; log SHA-256
  `53bfe4eaa44a5e1ce4f0d9f0cabeefa34422d906910db498673f86fcc9ec8bfa`.
- `./scripts/ci_local.sh`: PASS across all eight packages; log SHA-256
  `8380827cd397b3eb80307251a35c44d19d99f343eca03b5aff9fb16777ad0ac8`.
- Fixture byte comparison against the accepted docs worktree: PASS.
- `git diff --check`: PASS.

The earlier `bb26a45` review is non-acceptance history because that candidate
still pinned the superseded 12-scenario fixture. A fresh exact-HEAD review is
required after this correction is committed and pushed.
