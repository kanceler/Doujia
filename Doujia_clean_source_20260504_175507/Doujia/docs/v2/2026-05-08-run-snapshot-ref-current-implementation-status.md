# Run Snapshot Ref Current Implementation Status

> Date: 2026-05-08
>
> Scope: summarize the implementation state after Phase 1, Phase 2, and the current Phase 3 worktree implementation, so Phase 4 can start from a clear baseline.

---

## 1. Plain Summary

The refactor has moved from "task status is the main truth" toward "committed snapshot facts on the active ref are the scheduler input."

Current state in the Phase 3 worktree:

- `task` is still the execution unit.
- agent/runtime protocol is still `core.TaskMetaData`.
- feedback is committed to DoujiaGit facts before compatible scheduling decisions.
- root OK and pipeline-instance OK continuation can be explained from active ref member snapshots.
- active ref member consumption is recorded separately from immutable snapshots.
- full frontier replacement algebra for fanout, merge, call-return, and recover is not done yet.

In direct terms: the ledger is now strong enough to drive simple advancement, but not yet strong enough to legally replace multi-member frontiers.

---

## 2. Phase 1 Status: Fact Layer Clarified

Implemented shape:

- `core.TaskMetaData` has scheduler provenance fields:
  - `SourceSnapshotID`
  - `SourceSnapshotVersionID`
  - `SourceFrontierSnapshotID`
  - `SourceRefName`
  - `ContinuationID`
  - `DecisionKind`

- `doujiagit.TaskSnapshot` has minimal snapshot-version semantics:
  - `LogicalSnapshotID`
  - `SnapshotVersionID`
  - `SnapshotVersionNo`
  - `ArrivalKind`

- `doujiagit.Ref` now distinguishes:
  - `FrontierSnapshotID`: the current frontier snapshot record ID
  - `FrontierMemberSnapshotIDs`: concrete task snapshot members of the current frontier
  - `FrontierSnapshotIDs`: legacy compatibility alias for old JSON/tests/archive

- SQLite and memory repositories persist the new fields.
- archive/debug/query surfaces preserve the new fields.

What this means:

- `Ref.FrontierSnapshotID` is no longer confused with concrete member snapshots.
- `FrontierMemberSnapshotIDs` is the field new scheduling logic should read.
- `TaskMetaData` records why a dispatch happened, but does not become the source of runtime truth.

---

## 3. Phase 2 Status: Feedback Fact Commit Boundary

Implemented shape:

- `feedbackFactContext` exists as the internal handoff from fact commit to legacy-compatible scheduling.
- feedback fact commit is split from task-state mutation:
  - `commitFeedbackFact(...)`
  - `updateTaskStateFromFeedback(...)`

- feedback with a commit receipt writes:
  - artifact bags
  - task snapshot
  - frontier snapshot
  - ref movement

- dispatch helpers can populate provenance from the committed fact:
  - source snapshot
  - source snapshot version
  - source frontier snapshot
  - source ref
  - decision kind
  - continuation ID

What this means:

- compatible feedback paths no longer need to guess the latest runtime fact from task status alone.
- the old task-driven loop still exists, but it can run on top of committed facts.

---

## 4. Phase 3 Status: Active Ref Consumption Started

Implemented shape in the Phase 3 worktree:

- New DoujiaGit record:

```go
type SnapshotProcessingDecision struct {
	DecisionID                  string
	RunID                       core.RunID
	RefName                     string
	SnapshotID                  string
	SnapshotVersionID           string
	Status                      string
	DecisionKind                string
	ContinuationID              string
	Reason                      string
	ProducedTaskIDs             []string
	ProducedPipelineInstanceIDs []string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}
```

- Supported decision statuses:
  - `advanced`
  - `terminal`

- Repository APIs:
  - `CreateSnapshotProcessingDecision`
  - `GetSnapshotProcessingDecision`
  - `ListSnapshotProcessingDecisions`

- SQLite table:
  - `snapshot_processing_decisions`
  - unique by `(run_id, ref_name, snapshot_id)`

- Orchestrator active-ref helpers:
  - `activeRefMembers`
  - `snapshotAlreadyConsumed`
  - `advanceActiveRef`
  - `advanceActiveRefMember`
  - `recordSnapshotProcessingDecision`

Implemented scheduling behavior:

- root OK feedback with committed fact:
  - reads active ref member snapshot
  - dispatches next root task from that snapshot
  - records `advanced`
  - re-running active-ref consumption does not duplicate dispatch

- pipeline-instance OK feedback with committed fact:
  - reads active ref member snapshot
  - reconstructs output bag names from snapshot runtime context
  - merges instance output bags
  - advances the pipeline instance transition
  - records produced task IDs
  - re-running active-ref consumption does not duplicate dispatch

- terminal root feedback:
  - commits snapshot
  - records `terminal`
  - keeps legacy run failure behavior

- no active ref / no unconsumed member:
  - returns without failing or blocking the run

Archive/debug visibility:

- archive import/export includes processing decisions
- query graph includes `processing_decisions`
- debug HTTP exposes processing decisions through graph output
- debug UI has a processing-decision count

---

## 5. Known Phase 3 Compatibility Compromises

These are intentional boundaries, not accidental omissions:

1. `kbug`, repair-handler success, recover, and retry paths still use existing helper flow.
   - They still commit facts first.
   - They are not yet fully active-ref frontier replacement flows.

2. `advanceReadyTasks()` still exists.
   - It remains nil-DoujiaGit fallback.
   - It remains compatibility support for paths not yet migrated.

3. Multi-member frontier legality is not enforced yet.
   - Phase 3 can consume simple active ref members.
   - Phase 4 must decide how a consumed frontier member is replaced in a multi-member set.

4. Processing decisions mark consumption, but they do not yet encode complete frontier replacement semantics.
   - `ProducedTaskIDs` exists.
   - `ProducedPipelineInstanceIDs` exists.
   - There is not yet a normalized "from members -> to members" decision model for fanout/merge/call-return/recover.

---

## 6. Verification Status

Confirmed passing in the Phase 3 worktree:

```bash
go test ./internal/doujiagit/... -count=1
```

```bash
go test ./internal/orchestrator -run 'TestActiveRef|TestOnFeedback.*ActiveRef|TestOnFeedbackCommitReceiptCreatesDoujiaGitSnapshot|TestPipelineInstanceTaskFeedbackEntersTransitionToStateBeforeCompletion|TestCommitFeedbackFact|TestOnFeedbackFailureWithCommitReceiptCreatesFactBeforeRunFails|TestPipelineBugBubblesToParentReceivedHandler' -count=1
```

```bash
go test ./internal/core/... ./internal/doujiagit/... -count=1
```

Known broader orchestrator blockers:

- some legacy-oriented full-delivery tests still assume the old pre-JSON task/control semantics even after the real registry moved to `internal/orchestrator/testdata/full_delivery/pipeline_full_delivery.spec.json`
- PhaseTwoStub tests still reference task IDs such as `ceo_write_requirement` that are not present in those runs

These are not Phase 3 active-ref-consumption failures.

---

## 7. The Exact Starting Point for Phase 4

Phase 4 should start from this sentence:

> Active ref members can now be consumed once. Phase 4 must define and implement the legal replacement of the active frontier member set after that consumption.

That means Phase 4 should answer:

1. When one snapshot advances, which old frontier members are removed?
2. Which new members are added?
3. How do fanout-created children become frontier members?
4. When do multiple members merge into one?
5. How does child pipeline completion return into the parent frontier?
6. How does recover or repair replace a failed/debug frontier member without erasing history?

---

## 8. Phase 5 First Landing Status

Phase 5 now retires the nil-DoujiaGit scheduler fallback in the implementation worktree.

Implemented:

- Added `ErrDoujiaGitRequired` and `requireDoujiaGit()` in orchestrator.
- Added `advanceByFacts()` as the scheduling entrypoint for fact-mode advancement.
- Replaced production `advanceReadyTasks()` call sites with fact-mode advancement or direct fact-compatible dispatch.
- Changed core active-ref helpers so missing DoujiaGit is an error instead of silent no-op.
- Changed `commitFeedbackFact()` so a configured repository is required.
- Preserved runtime compatibility for feedback without `CommitReceipt` by writing a zero-output `TaskSnapshot` fact instead of falling back to task-status scheduling.
- Added `runFactProjection` and conservative legacy status projection sync.
- Added tests proving:
  - missing DoujiaGit returns `ErrDoujiaGitRequired`
  - legacy ready tasks are ignored without active-ref facts
  - active ref projection distinguishes unconsumed, consumed, failed, and stale legacy status
  - stale failed run status can be corrected when facts do not prove failure
- Migrated dynamic control first-dispatch behavior away from `advanceReadyTasks()`.
- Added a narrow dynamic-dependent dispatch helper for already materialized dynamic tasks after fact-first feedback.

Known remaining test failures outside this first landing:

- Several older orchestrator integration tests still need fixture/expectation migration now that the real full-delivery registry lives under `internal/orchestrator/testdata/full_delivery` and the active flow semantics are JSON-first.
- Two PhaseTwo stub full-flow tests still fail because they reference task IDs such as `ceo_write_requirement` that are not created by the current stub fixture.

Verified passing:

```bash
go test ./internal/doujiagit/... -count=1
```

```bash
go test ./internal/orchestrator -run 'TestRequireDoujiaGit|TestRunFactProjection|Test.*Stale.*Status|Test.*DoujiaGitRequired|Test.*LegacyReadyTaskIgnored|TestPipelineInstance.*FactTruth|TestPhaseTwoSplitModuleControlCreatesDynamic|TestDynamic.*Debug|TestChildFeedbackRedispatchesParentWithMergedArtifacts' -count=1
```

```bash
go test ./internal/core/... -count=1
```
