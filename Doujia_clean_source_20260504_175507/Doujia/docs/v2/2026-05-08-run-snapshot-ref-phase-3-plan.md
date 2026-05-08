# Run Snapshot Ref Phase 3 Implementation Notes

Phase 3 switches DoujiaGit-configured root OK and pipeline-instance OK continuation to active-ref member consumption while keeping task dispatch as the execution unit.

Implemented boundary:

- Snapshot processing decisions are persisted separately from immutable `TaskSnapshot` facts.
- `advanced` and `terminal` decisions count as consumed.
- Root OK feedback commits a snapshot, moves the active ref, consumes that ref member, dispatches the next task with source provenance, and records the produced task ID.
- Pipeline-instance OK feedback commits a snapshot, moves the active ref, consumes that ref member, merges instance output bags from the snapshot context, advances the transition, and records produced task IDs.
- Terminal root feedback records a `terminal` processing decision before the run is marked failed.
- No active ref or no unconsumed active-ref member returns without failing or blocking the run.
- Nil DoujiaGit remains task-driven compatibility behavior.

Compatibility compromise:

- `kbug`, repair-handler success, and recover/retry paths remain on the existing helper flow in Phase 3. They still commit feedback facts first, but full recover/frontier replacement algebra is deferred to Phase 4.
- Phase 3 does not implement full fanout, merge, call-return, or recover frontier replacement semantics.

Verification notes:

- `go test ./internal/doujiagit/... -count=1` passes.
- Focused active-ref orchestrator tests pass.
- `go test ./internal/orchestrator/... -count=1` is still blocked by known unrelated fixture/stub failures:
  - missing `docs/v2/pipeline_full_delivery.spec.json`
  - PhaseTwoStub tests referencing `ceo_write_requirement` task IDs that are not present.
